package httpadapter

import "testing"

func TestStatusError_Error(t *testing.T) {
	err := &StatusError{StatusCode: 404, Status: "404 Not Found"}
	if got := err.Error(); got == "" {
		t.Error("Error() returned empty string")
	}
}
