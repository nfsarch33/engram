package engramsvc

// MessageText extracts the text of one element of an add request's
// `messages` array. Writers send either a bare string or a chat-style
// object ({"role":"user","content":"..."} - the shape every other memory
// API takes); an object may carry its text as "content" or "text".
//
// Only the bare string used to be accepted. Objects were dropped on the
// floor, the request reached the service with zero messages, and every
// writer in the fleet that sent the object shape failed with "text must not
// be empty" for weeks while search kept working. Returns ok=false when the
// element carries no usable text.
func MessageText(v any) (string, bool) {
	switch m := v.(type) {
	case string:
		return m, m != ""
	case map[string]any:
		for _, key := range []string{"content", "text"} {
			if s, ok := m[key].(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

// MessageTexts applies MessageText to a raw array, keeping order and
// dropping elements with no usable text. The caller decides whether an
// empty result is an error (it is, for an add).
func MessageTexts(raw []any) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := MessageText(v); ok {
			out = append(out, s)
		}
	}
	return out
}
