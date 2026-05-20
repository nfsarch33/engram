package engramsvc_test

import "encoding/json"

// jsonUnmarshal wraps encoding/json so testutil_test.go stays clean.
var jsonUnmarshal = json.Unmarshal
