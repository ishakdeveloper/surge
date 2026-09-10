package stripe

import (
	"context"
	"fmt"

	stripego "github.com/stripe/stripe-go/v86"
)

// AccountSession lets the driver's browser render Stripe's embedded Connect
// components for their account.
//
// The notification banner only. Stripe asks every platform to show it: it is
// how a driver learns their account needs something — a document, a bank
// detail — before payouts stop, rather than after.
func (p *Processor) AccountSession(ctx context.Context, accountID string) (string, error) {
	session, err := p.client.V1AccountSessions.Create(ctx, &stripego.AccountSessionCreateParams{
		Account: stripego.String(accountID),
		Components: &stripego.AccountSessionCreateComponentsParams{
			NotificationBanner: &stripego.AccountSessionCreateComponentsNotificationBannerParams{
				Enabled: stripego.Bool(true),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("stripe: account session for %s: %w", accountID, err)
	}
	return session.ClientSecret, nil
}

// DashboardLink is a single-use link into the driver's Express dashboard,
// where Stripe shows them their payouts and lets them change their bank.
func (p *Processor) DashboardLink(ctx context.Context, accountID string) (string, error) {
	link, err := p.client.V1LoginLinks.Create(ctx, &stripego.LoginLinkCreateParams{
		Account: stripego.String(accountID),
	})
	if err != nil {
		return "", fmt.Errorf("stripe: dashboard link for %s: %w", accountID, err)
	}
	return link.URL, nil
}
