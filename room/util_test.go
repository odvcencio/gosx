package room

import (
	"encoding/json"
	"net/http"
)

func jsonDecode(resp *http.Response, v any) error { return json.NewDecoder(resp.Body).Decode(v) }
