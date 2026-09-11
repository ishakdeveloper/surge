package domain

// QuickReply is a message sent with one tap.
//
// Mostly for the driver, who reads the screen between junctions and should
// not be typing: one tap says "I'm here". The code is what is stored with the
// message, so a client can show it in its own language; Text is the English
// that becomes the body.
type QuickReply struct {
	Code string
	Text string
	Role Role
	Kind Kind
}

// quickReplies is the catalogue, in the order a client shows them.
var quickReplies = []QuickReply{
	{Code: "driver.on_my_way", Text: "On my way", Role: RoleDriver, Kind: KindTrip},
	{Code: "driver.arrived", Text: "I'm here", Role: RoleDriver, Kind: KindTrip},
	{Code: "driver.running_late", Text: "Running a few minutes late", Role: RoleDriver, Kind: KindTrip},
	{Code: "driver.cant_find_you", Text: "I can't find you. Where are you?", Role: RoleDriver, Kind: KindTrip},
	{Code: "rider.coming_out", Text: "Coming out now", Role: RoleRider, Kind: KindTrip},
	{Code: "rider.where_are_you", Text: "Where are you?", Role: RoleRider, Kind: KindTrip},
	{Code: "rider.wait_please", Text: "Please wait a moment", Role: RoleRider, Kind: KindTrip},
	{Code: "support.looking_into_it", Text: "Thanks, we're looking into it", Role: RoleSupport, Kind: KindSupport},
	{Code: "support.anything_else", Text: "Is there anything else we can help with?", Role: RoleSupport, Kind: KindSupport},
}

// QuickRepliesFor is what someone on this side of this kind of conversation
// can send with one tap.
func QuickRepliesFor(role Role, kind Kind) []QuickReply {
	var replies []QuickReply
	for _, reply := range quickReplies {
		if reply.Role == role && reply.Kind == kind {
			replies = append(replies, reply)
		}
	}
	return replies
}

// LookupQuickReply finds a code, and only among the replies this sender could
// have been offered: a rider cannot send the driver's "I'm here".
func LookupQuickReply(code string, role Role, kind Kind) (QuickReply, bool) {
	for _, reply := range quickReplies {
		if reply.Code == code && reply.Role == role && reply.Kind == kind {
			return reply, true
		}
	}
	return QuickReply{}, false
}
