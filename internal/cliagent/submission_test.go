package cliagent

import "testing"

func TestExtractSubmissionJSON(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		wantRC  string
		wantNil bool
	}{
		{
			name:   "fenced json block",
			text:   "Here is my analysis.\n\n```json\n{\"root_cause\":\"unbounded cache leak\",\"actions_taken\":\"set CACHE_MAX\",\"postmortem\":\"bounded it\"}\n```\nDone.",
			wantRC: "unbounded cache leak",
		},
		{
			name:   "bare object around root_cause",
			text:   "Summary: {\"root_cause\": \"regex backtracking pinned CPU\", \"actions_taken\": \"disabled rule\"} that's it",
			wantRC: "regex backtracking pinned CPU",
		},
		{
			name:    "no json at all",
			text:    "The service recovered after I fixed the slow query.",
			wantNil: true,
		},
		{
			name:    "json without root_cause is ignored",
			text:    "```json\n{\"note\":\"hi\"}\n```",
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSubmissionJSON(tc.text)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected a submission, got nil")
			}
			if got.RootCause != tc.wantRC {
				t.Errorf("root_cause = %q, want %q", got.RootCause, tc.wantRC)
			}
		})
	}
}
