package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// The Go half of the error contract.
//
// proto/common.proto declares ErrorBody, the published document describes it
// from that declaration, and the TypeScript client is generated from the
// document — so this handler emitting anything else is a client that fails to
// decode every error. That was the state of things until the proto declared
// the shape: the document said rpcStatus, this said {"error":{...}}, and
// nothing compared them.
//
// packages/domain/test/api/Generation.test.ts decodes errors captured from the
// running gateway. This pins the same shape without one.

func errorFor(t *testing.T, err error) (int, *commonpb.ErrorBody, string) {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/trips", nil)
	writeError(context.Background(), nil, &runtime.JSONPb{}, recorder, request, err)

	raw := recorder.Body.String()
	var body commonpb.ErrorBody
	if decodeErr := protojson.Unmarshal([]byte(raw), &body); decodeErr != nil {
		t.Fatalf("body is not an ErrorBody: %v\n%s", decodeErr, raw)
	}
	return recorder.Code, &body, raw
}

func TestErrorBodyIsTheProtoShape(t *testing.T) {
	code, body, _ := errorFor(t, status.Error(codes.NotFound, "unknown trip"))

	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
	if body.GetError().GetCode() != "not_found" || body.GetError().GetMessage() != "unknown trip" {
		t.Errorf("error = %+v", body.GetError())
	}
}

func TestUnauthenticatedIs401(t *testing.T) {
	code, body, _ := errorFor(t, status.Error(codes.Unauthenticated, "missing token"))

	if code != http.StatusUnauthorized || body.GetError().GetCode() != "unauthenticated" {
		t.Errorf("got %d %+v", code, body.GetError())
	}
}

// Internal messages carry table names, connection strings and file paths.
func TestInternalDetailStaysInTheTrace(t *testing.T) {
	code, body, _ := errorFor(t, status.Error(codes.Internal, `pq: relation "trip" does not exist`))

	if code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", code)
	}
	if strings.Contains(body.GetError().GetMessage(), "relation") {
		t.Errorf("leaked internal detail: %q", body.GetError().GetMessage())
	}
}

// The generated client treats every field as required, because the gateway
// marshals responses with EmitUnpopulated. The error marshaler has to hold the
// same line: an empty message must still be present as a key, or the client's
// decoder rejects an error whose only fault was having nothing to say.
func TestEmptyMessageIsStillPresent(t *testing.T) {
	_, _, raw := errorFor(t, status.Error(codes.NotFound, ""))

	if !strings.Contains(raw, `"message":""`) {
		t.Errorf("an empty message was omitted, which the client cannot decode: %s", raw)
	}
}

// The TypeScript client decodes an error body only for the statuses it was told
// about, and scripts/generate-api-client.mjs is what tells it — by listing the
// statuses this file maps gRPC codes to. A status added to httpStatus and not
// there reaches browsers as an undecoded StatusCodeError. This reads the
// script's list and fails when the two disagree, in either direction.
func TestEveryStatusTheGatewayEmitsIsDeclaredToClients(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "..", "scripts", "generate-api-client.mjs")
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the client generator: %v", err)
	}

	match := regexp.MustCompile(`const GATEWAY_ERROR_STATUSES = \[([0-9,\s]+)\];`).FindSubmatch(script)
	if match == nil {
		t.Fatal("GATEWAY_ERROR_STATUSES not found in scripts/generate-api-client.mjs")
	}

	declared := map[int]bool{}
	for _, field := range strings.FieldsFunc(string(match[1]), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		status, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("not a status: %q", field)
		}
		declared[status] = true
	}

	// 500 is what any code missing from the table becomes.
	emitted := map[int]bool{http.StatusInternalServerError: true}
	for _, status := range httpStatus {
		emitted[status] = true
	}

	for status := range emitted {
		if !declared[status] {
			t.Errorf("the gateway can answer %d, but the client generator does not declare it", status)
		}
	}
	for status := range declared {
		if !emitted[status] {
			t.Errorf("the client generator declares %d, but the gateway never answers with it", status)
		}
	}
}
