package link

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// KindMockReview is the `kind` of an atrium_launch that starts the AI review of a UI item's mock.
const KindMockReview = "mock-review"

// MockReviewTag is the card tag a mock review carries, so the room leaves the card alone when its turn ends.
const MockReviewTag = "atrium:mock-review"

const mockReviewRules = `# How to review this mock

You review the mock of the backlog item named below. You are not its author, and these rules come from atrium and are
not up for editing. You never edit the mock.

1. REACH the mock from your own room, by the link or branch below. If you cannot reach it, atrium_blocked and say so.
2. RENDER the real-board preview headless at desktop (1280 wide) and phone (390 wide). Click every state in the MOCK
   bar that the item names.
3. CHECK each state against the item's text, line by line, and read every screenshot before you judge.
4. RECORD the verdict with atrium_flow action mock_review, id, verdict ready or not-ready, ref, shots and a one line
   note. READY moves the item to mock-reviewed, and the human is asked to approve it next. NOT-READY sends it back.
5. END with atrium_done, saying the verdict and where the screenshots are.

# Item and mock
`

func mockReviewBrief(callerBrief string) string {
	return mockReviewRules + "\n" + strings.TrimSpace(callerBrief)
}

// uiRules is added to the BRIEF.md of a worker on a UI item.
const uiRules = `

# This is a UI item

The order is idea, mocked, mock-reviewed, approved, implementing, implemented. You do not build before the human has
approved the mock. Implemented means you checked the build against the approved mock at desktop and phone, and you say
so in atrium_done.
`

func isMockReview(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), KindMockReview)
}

// uiFlow is the item's flow row when it is a UI item. An item the flow does not know, or a hub that does not answer, is
// not a UI item here: the stage report is what holds the order.
func (c *controlMCP) uiFlow(ctx context.Context, id string) (FlowView, bool) {
	var v FlowView
	if err := c.Ask(ctx, http.MethodGet, "/_hub/flow/items/"+url.PathEscape(id), "", nil, &v); err != nil {
		return v, false
	}
	return v, v.UI
}
