package httpapi

import "testing"

func TestJMAPSubmissionRcpts(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"no submission", `{"methodCalls":[["Email/get",{},"0"]]}`, 0},
		{"envelope with 3 rcpts", `{"methodCalls":[["EmailSubmission/set",{"create":{"s1":{"emailId":"e1","envelope":{"mailFrom":{"email":"a@x"},"rcptTo":[{"email":"b@x"},{"email":"c@x"},{"email":"d@x"}]}}}},"0"]]}`, 3},
		{"no envelope counts one", `{"methodCalls":[["EmailSubmission/set",{"create":{"s1":{"emailId":"e1"}}},"0"]]}`, 1},
		{"two creates mixed", `{"methodCalls":[["EmailSubmission/set",{"create":{"a":{"envelope":{"rcptTo":[{"email":"b@x"}]}},"b":{}}},"0"]]}`, 2},
		{"multiple calls", `{"methodCalls":[["Email/set",{},"0"],["EmailSubmission/set",{"create":{"s":{"envelope":{"rcptTo":[{"email":"b@x"},{"email":"c@x"}]}}}},"1"]]}`, 2},
		{"invalid json", `not json`, 0},
	}
	for _, c := range cases {
		if got := jmapSubmissionRcpts([]byte(c.body)); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
