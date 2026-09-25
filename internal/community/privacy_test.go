package community

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func nowStub() time.Time { return time.Unix(0, 0).UTC() }

// Exit criterion: "Private tenant data is not revealed through community
// profiles/messages." The architectural guarantee is that community response
// types have no organization, cost, price, or revenue fields at all. This test
// pins that so a future field addition cannot silently leak tenant data.
func TestCommunityPayloadsCarryNoTenantData(t *testing.T) {
	post := Post{
		ID: "p1", RoomID: "r1", RoomSlug: "store-owners", AuthorUserID: "u1",
		AuthorName: "Ada", Body: "How do you handle promotions?", Status: "visible", CreatedAt: nowStub(),
	}
	reply := Reply{ID: "rp1", PostID: "p1", AuthorUserID: "u1", AuthorName: "Ada", Body: "Bundles worked for me."}
	profile := Profile{UserID: "u1", DisplayName: "Ada", Headline: "Owner", Bio: "Lagos", PrivacyLevel: "minimal", MemberSince: 2}
	room := Room{ID: "r1", Slug: "store-owners", Name: "Store owners", Kind: "owner"}

	for _, value := range []any{post, reply, profile, room} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		lowered := strings.ToLower(string(raw))
		for _, forbidden := range []string{
			"organization", "org_id", "cost", "price_kobo", "supplier", "landed",
			"revenue", "margin_bps", "store_url", "consumer_key", "api_key",
		} {
			if strings.Contains(lowered, forbidden) {
				t.Errorf("community payload leaked %q: %s", forbidden, raw)
			}
		}
	}
}

func TestProfileHasNoOrganizationField(t *testing.T) {
	raw, err := json.Marshal(Profile{UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "org") {
		t.Errorf("profile must not reference an organization: %s", raw)
	}
}

func TestModerationItemRedactsSensitiveFigures(t *testing.T) {
	// Auto-blocked content is surfaced to moderators with figures redacted.
	verdict := ScreenContent("our competitor's cost is 4500 and margin 60%")
	if verdict.Allowed {
		t.Fatal("competitor cost disclosure should be blocked")
	}
	item := ModerationItem{Reason: "auto-blocked: " + string(verdict.Category) + " (" + Excerpt("our competitor's cost is 4500") + ")"}
	if categoryFromReason(item.Reason) != string(ViolationCompetitorDeal) {
		t.Errorf("category not extracted: %q", categoryFromReason(item.Reason))
	}
	if strings.Contains(item.Reason, "4500") {
		t.Errorf("moderation excerpt must redact figures: %q", item.Reason)
	}
}

func TestMentionExtractionUsesHandlesNotEmails(t *testing.T) {
	mentions := extractMentions("ping @ada_store about this, not owner@example.com")
	for _, m := range mentions {
		if strings.Contains(m, "@") || strings.Contains(m, ".") {
			t.Errorf("mention must be a bare handle, got %q", m)
		}
		if m == "ada_store" {
			return
		}
	}
	t.Fatalf("expected handle ada_store, got %v", mentions)
}

func TestMentionExtractionNeverReturnsEmpty(t *testing.T) {
	if got := extractMentions("no mentions here"); len(got) != 1 {
		t.Fatalf("expected sentinel for empty array, got %v", got)
	}
}
