package repository_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/shared/pgtest"
)

var t0 = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func tripConversation(t *testing.T, repo *repository.Postgres, id, tripID string, closesAt time.Time) *domain.Conversation {
	t.Helper()
	conversation := domain.NewTripConversation(id, tripID, "rider-1", "drv-1", t0)
	conversation.ClosesAt = closesAt
	stored, _, err := repo.EnsureTrip(context.Background(), conversation)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	return stored
}

func message(conversationID, sender, key string, at time.Time) domain.Message {
	return domain.Message{
		ID: "m-" + conversationID + "-" + key, ConversationID: conversationID,
		SenderID: sender, SenderRole: domain.RoleRider, Body: "hello",
		ClientMessageID: key, CreatedAt: at,
	}
}

// The property the whole read path rests on: seqs without gaps or repeats,
// however many sends race. A client reading after the newest seq it holds
// then misses nothing.
func TestConcurrentSendsNumberWithoutGaps(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", time.Time{})

	const sends = 20
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seqs []int64
	)
	for i := range sends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, err := repo.Append(ctx, message(conversation.ID, "rider-1", fmt.Sprint(i), t0), t0)
			if err != nil {
				t.Errorf("send %d: %v", i, err)
				return
			}
			mu.Lock()
			seqs = append(seqs, stored.Seq)
			mu.Unlock()
		}()
	}
	wg.Wait()

	slices.Sort(seqs)
	for i, seq := range seqs {
		if seq != int64(i+1) {
			t.Fatalf("seqs %v, want 1..%d", seqs, sends)
		}
	}

	stored, _ := repo.Get(ctx, conversation.ID)
	if stored.LastSeq != sends {
		t.Errorf("last seq %d", stored.LastSeq)
	}
	if rider, _ := stored.Participant("rider-1"); rider.LastReadSeq != sends {
		t.Errorf("the sender has read to %d", rider.LastReadSeq)
	}
	jobs, _ := repo.LeasePushes(ctx, t0, t0.Add(time.Minute), 100)
	if len(jobs) != sends || jobs[0].RecipientID != "drv-1" {
		t.Errorf("%d notifications owed, want one per message to the driver", len(jobs))
	}
}

// A retry after a lost response is the message it retried, and gives its seq
// back rather than leaving a gap.
func TestARetriedSendIsOneMessage(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", time.Time{})

	first, err := repo.Append(ctx, message(conversation.ID, "rider-1", "k", t0), t0)
	if err != nil {
		t.Fatal(err)
	}
	retry := message(conversation.ID, "rider-1", "k", t0.Add(time.Second))
	retry.ID = "another-id"
	again, err := repo.Append(ctx, retry, t0)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.Seq != 1 {
		t.Errorf("the retry stored %+v", again)
	}

	next, _ := repo.Append(ctx, message(conversation.ID, "rider-1", "k2", t0), t0)
	if next.Seq != 2 {
		t.Errorf("the next message is seq %d: the retry left a gap", next.Seq)
	}
}

func TestSendingStopsAtTheClose(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", t0.Add(time.Hour))

	if _, err := repo.Append(ctx, message(conversation.ID, "rider-1", "in-time", t0.Add(59*time.Minute)), t0); err != nil {
		t.Errorf("inside the window: %v", err)
	}
	if _, err := repo.Append(ctx, message(conversation.ID, "rider-1", "late", t0.Add(time.Hour)), t0); !errors.Is(err, domain.ErrClosed) {
		t.Errorf("at the close: %v", err)
	}
	if _, err := repo.Append(ctx, message("nope", "rider-1", "k", t0), t0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no such conversation: %v", err)
	}
}

// The consumer and a rider's first tap may both create it, and facts may
// arrive late or twice: whichever lands first, one conversation, closed by the
// first end.
func TestATripHasOneConversationClosedByItsFirstEnd(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()

	ended := tripConversation(t, repo, "from-completion", "trip-1", t0.Add(time.Hour))
	accepted := domain.NewTripConversation("from-acceptance", "trip-1", "rider-1", "drv-1", t0)
	stored, changed, err := repo.EnsureTrip(ctx, accepted)
	if err != nil {
		t.Fatal(err)
	}
	if changed || stored.ID != ended.ID {
		t.Errorf("a late acceptance changed=%v and found %s", changed, stored.ID)
	}
	if !stored.ClosesAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("the acceptance reopened it: closes %v", stored.ClosesAt)
	}

	redelivered := domain.NewTripConversation("again", "trip-1", "rider-1", "drv-1", t0)
	redelivered.ClosesAt = t0.Add(3 * time.Hour)
	if stored, changed, _ = repo.EnsureTrip(ctx, redelivered); changed || !stored.ClosesAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("a second end moved the close to %v", stored.ClosesAt)
	}
	if len(stored.Participants) != 2 {
		t.Errorf("participants %+v", stored.Participants)
	}

	byTrip, err := repo.GetByTrip(ctx, "trip-1")
	if err != nil || byTrip.ID != ended.ID {
		t.Errorf("by trip: %v %v", byTrip, err)
	}
}

func TestReadMarkersOnlyMoveForward(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", time.Time{})

	if at, moved, err := repo.MarkRead(ctx, conversation.ID, "drv-1", 3); err != nil || !moved || at != 3 {
		t.Fatalf("to 3: at=%d moved=%v err=%v", at, moved, err)
	}
	if at, moved, _ := repo.MarkRead(ctx, conversation.ID, "drv-1", 2); moved || at != 3 {
		t.Errorf("back to 2: at=%d moved=%v", at, moved)
	}
	if _, _, err := repo.MarkRead(ctx, conversation.ID, "rider-2", 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("someone not in it: %v", err)
	}
}

func TestSupportIsCreatedOnceAndClaimedOnce(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()

	conversation := domain.NewSupportConversation("s1", "rider-1", domain.RoleRider, "", "Lost bag", t0)
	first := message("s1", "rider-1", "help-1", t0)
	first.Seq = 1
	created, isNew, err := repo.CreateSupport(ctx, conversation, first)
	if err != nil || !isNew {
		t.Fatalf("create: new=%v err=%v", isNew, err)
	}
	if created.LastSeq != 1 || created.Subject != "Lost bag" {
		t.Errorf("created %+v", created)
	}

	retry := domain.NewSupportConversation("s2", "rider-1", domain.RoleRider, "", "Lost bag", t0)
	again, isNew, err := repo.CreateSupport(ctx, retry, first)
	if err != nil || isNew || again.ID != "s1" {
		t.Errorf("a retried create: id=%s new=%v err=%v", again.ID, isNew, err)
	}

	support := domain.Participant{UserID: "ops-1", Role: domain.RoleSupport}
	claimed, err := repo.Claim(ctx, "s1", support, t0)
	if err != nil || claimed.AssigneeID != "ops-1" {
		t.Fatalf("claim: %v", err)
	}
	if _, in := claimed.Participant("ops-1"); !in {
		t.Errorf("the assignee is not in the conversation")
	}
	if _, err := repo.Claim(ctx, "s1", domain.Participant{UserID: "ops-2", Role: domain.RoleSupport}, t0); !errors.Is(err, domain.ErrClaimed) {
		t.Errorf("a second agent: %v", err)
	}
	if _, err := repo.Claim(ctx, "s1", support, t0); err != nil {
		t.Errorf("claiming your own again: %v", err)
	}

	resolved, err := repo.Resolve(ctx, "s1", t0)
	if err != nil || resolved.Status != domain.StatusResolved {
		t.Errorf("resolve: %v", err)
	}
}

func TestListFiltersAndPages(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()

	tripConversation(t, repo, "open", "trip-1", time.Time{})
	closed := domain.NewTripConversation("closed", "trip-2", "rider-1", "drv-2", t0.Add(time.Minute))
	closed.ClosesAt = t0
	if _, _, err := repo.EnsureTrip(ctx, closed); err != nil {
		t.Fatal(err)
	}
	support := domain.NewSupportConversation("help", "rider-2", domain.RoleRider, "", "", t0.Add(2*time.Minute))
	if _, _, err := repo.CreateSupport(ctx, support, message("help", "rider-2", "k", t0)); err != nil {
		t.Fatal(err)
	}

	ids := func(filter domain.ListFilter) []string {
		t.Helper()
		filter.Now = t0.Add(time.Hour)
		page, err := repo.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range page.Conversations {
			out = append(out, c.ID)
		}
		return out
	}

	if got := ids(domain.ListFilter{UserID: "rider-1"}); !slices.Equal(got, []string{"closed", "open"}) {
		t.Errorf("the rider's, newest first: %v", got)
	}
	if got := ids(domain.ListFilter{UserID: "rider-1", State: domain.StateOpen}); !slices.Equal(got, []string{"open"}) {
		t.Errorf("the rider's open ones: %v", got)
	}
	if got := ids(domain.ListFilter{Kind: domain.KindSupport}); !slices.Equal(got, []string{"help"}) {
		t.Errorf("support's queue: %v", got)
	}

	page, err := repo.List(ctx, domain.ListFilter{UserID: "rider-1", Limit: 1, Now: t0})
	if err != nil || len(page.Conversations) != 1 || page.NextCursor != "closed" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	next, _ := repo.List(ctx, domain.ListFilter{UserID: "rider-1", Limit: 1, Cursor: page.NextCursor, Now: t0})
	if len(next.Conversations) != 1 || next.Conversations[0].ID != "open" || next.NextCursor != "" {
		t.Errorf("second page: %+v", next)
	}
}

func TestMessagesPageBothWays(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", time.Time{})
	for i := range 5 {
		if _, err := repo.Append(ctx, message(conversation.ID, "rider-1", fmt.Sprint(i), t0), t0); err != nil {
			t.Fatal(err)
		}
	}

	seqs := func(query domain.MessageQuery) ([]int64, bool) {
		messages, more, err := repo.Messages(ctx, conversation.ID, query)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, m := range messages {
			out = append(out, m.Seq)
		}
		return out, more
	}

	if got, more := seqs(domain.MessageQuery{Limit: 2}); !slices.Equal(got, []int64{4, 5}) || !more {
		t.Errorf("the newest page: %v more=%v", got, more)
	}
	if got, more := seqs(domain.MessageQuery{Before: 4, Limit: 2}); !slices.Equal(got, []int64{2, 3}) || !more {
		t.Errorf("back from 4: %v more=%v", got, more)
	}
	if got, more := seqs(domain.MessageQuery{After: 3}); !slices.Equal(got, []int64{4, 5}) || more {
		t.Errorf("after 3: %v more=%v", got, more)
	}

	if sent, _ := repo.SentSince(ctx, "rider-1", t0.Add(-time.Minute)); sent != 5 {
		t.Errorf("sent in the last minute: %d", sent)
	}
}

// A leased job is hidden from every other worker until its lease runs out,
// which is what lets every instance run a worker.
func TestALeaseHidesAJob(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	conversation := tripConversation(t, repo, "c1", "trip-1", time.Time{})
	if _, err := repo.Append(ctx, message(conversation.ID, "rider-1", "k", t0), t0.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}

	if early, _ := repo.LeasePushes(ctx, t0, t0.Add(time.Minute), 10); len(early) != 0 {
		t.Errorf("leased before it was due")
	}
	due := t0.Add(8 * time.Second)
	jobs, err := repo.LeasePushes(ctx, due, due.Add(30*time.Second), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("lease: %v %v", jobs, err)
	}
	if again, _ := repo.LeasePushes(ctx, due, due.Add(30*time.Second), 10); len(again) != 0 {
		t.Errorf("a leased job was leased twice")
	}

	if err := repo.RetryPushes(ctx, []int64{jobs[0].ID}, due); err != nil {
		t.Fatal(err)
	}
	retried, _ := repo.LeasePushes(ctx, due, due.Add(30*time.Second), 10)
	if len(retried) != 1 || retried[0].Attempts != 1 {
		t.Fatalf("after a retry: %+v", retried)
	}
	if err := repo.FinishPushes(ctx, []int64{jobs[0].ID}); err != nil {
		t.Fatal(err)
	}
	if left, _ := repo.LeasePushes(ctx, due.Add(time.Hour), due.Add(2*time.Hour), 10); len(left) != 0 {
		t.Errorf("a finished job came back")
	}
}

func TestPushTokensFollowTheDevice(t *testing.T) {
	repo := repository.NewPostgres(pgtest.Pool(t))
	ctx := context.Background()
	token := domain.PushToken{Token: "ExponentPushToken[a]", UserID: "rider-1", Platform: domain.PlatformIOS, UpdatedAt: t0}

	if err := repo.SavePushToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	token.UserID = "drv-1"
	if err := repo.SavePushToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if theirs, _ := repo.PushTokens(ctx, "rider-1"); len(theirs) != 0 {
		t.Errorf("the rider kept the phone")
	}
	if err := repo.DeletePushToken(ctx, "rider-1", token.Token); err != nil {
		t.Fatal(err)
	}
	if theirs, _ := repo.PushTokens(ctx, "drv-1"); len(theirs) != 1 {
		t.Errorf("the rider signed the driver's phone out")
	}
	if err := repo.DeletePushTokens(ctx, []string{token.Token}); err != nil {
		t.Fatal(err)
	}
	if theirs, _ := repo.PushTokens(ctx, "drv-1"); len(theirs) != 0 {
		t.Errorf("a pruned token survived")
	}
}
