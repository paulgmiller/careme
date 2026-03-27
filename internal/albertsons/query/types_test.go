package query

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPathwaySearchPayloadUnmarshalFixture(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("acmeresp.json")
	if err != nil {
		t.Fatalf("ReadFile(acmeresp.json) returned error: %v", err)
	}

	var payload PathwaySearchPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if payload.Response.NumFound != 194 {
		t.Fatalf("unexpected numFound: %d", payload.Response.NumFound)
	}
	if len(payload.Response.Docs) == 0 {
		t.Fatal("expected docs to be populated")
	}
	if payload.Response.Docs[0].StoreID != "806" {
		t.Fatalf("unexpected first doc storeId: %q", payload.Response.Docs[0].StoreID)
	}
	if payload.Response.Docs[0].ChannelEligibility.Delivery != true {
		t.Fatalf("expected first doc delivery eligibility to be true")
	}

}
