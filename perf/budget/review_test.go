package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReviewExceptionDigestsStrictHistoricalBlob(t *testing.T) {
	file, err := Load("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	file.Exceptions = []Exception{admissionException()}
	data, _ := json.Marshal(file)
	digests, err := ReviewExceptionDigests(bytes.NewReader(data))
	want, _ := ExceptionSHA256(file.Exceptions[0])
	if err != nil || digests[file.Exceptions[0].ID] != want {
		t.Fatal("review did not bind complete exception content", err)
	}
	for _, cause := range []string{"duplicate-key", "duplicate-id", "unknown-field", "utf8", "trailing", "too-large"} {
		t.Run(cause, func(t *testing.T) {
			invalid := append([]byte{}, data...)
			switch cause {
			case "duplicate-key":
				invalid = bytes.Replace(invalid, []byte(`"exceptions":`), []byte(`"exceptions":[],"exceptions":`), 1)
			case "duplicate-id":
				file.Exceptions = append(file.Exceptions, file.Exceptions[0])
				invalid, _ = json.Marshal(file)
			case "unknown-field":
				invalid = append([]byte(`{"private":"value",`), invalid[1:]...)
			case "utf8":
				invalid = append(invalid, 0xff)
			case "trailing":
				invalid = append(invalid, []byte("{}")...)
			case "too-large":
				invalid = bytes.Repeat([]byte(" "), maxInputBytes+1)
			}
			_, err := ReviewExceptionDigests(bytes.NewReader(invalid))
			var typed *InputError
			if !errors.As(err, &typed) || typed.Reference != "review" || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid review blob accepted or leaked content", err)
			}
		})
	}
	if _, err := ReviewExceptionDigests(nil); err == nil {
		t.Fatal("nil reviewed blob accepted")
	}
}
