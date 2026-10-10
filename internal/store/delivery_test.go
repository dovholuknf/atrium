package store

import (
	"testing"
)

func TestDeliveryKindAndReply(t *testing.T) {
	for _, c := range []struct{ from, cause, want string }{
		{"", "", DeliveryOperator}, {"atrium", "", DeliveryNotice}, {"w-1", "", DeliverySay},
		{"w-1", DeliveryReport, DeliveryReport}, {"atrium", DeliveryNudge, DeliveryNudge}, {"", DeliveryNotice, DeliveryNotice},
	} {
		if got := DeliveryKind(c.from, c.cause); got != c.want {
			t.Errorf("DeliveryKind(%q, %q) = %q, want %q", c.from, c.cause, got, c.want)
		}
	}
	for _, c := range []struct {
		text string
		tool bool
		want string
	}{
		{"", false, ReplyNone}, {"  \n", false, ReplyNone}, {"Got it.", false, ReplyAck}, {"OK, thanks!", false, ReplyAck},
		{"No response requested.", false, ReplyAck}, {"👍", false, ReplyAck},
		{"Got it. I will now rewrite the scheduler so that it no longer walks every card on each tick and instead keeps a heap", false, ReplyText},
		{"The build failed because the fixture moved.", false, ReplyText}, {"ok", true, ReplyWork}, {"", true, ReplyWork},
	} {
		if got := ClassifyReply(c.text, c.tool); got != c.want {
			t.Errorf("ClassifyReply(%q, %v) = %q, want %q", c.text, c.tool, got, c.want)
		}
	}
}
