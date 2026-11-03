package group

import (
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

func TestURLTestObjective(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		options option.ProviderURLTestOptions
		want    string
		wantErr bool
	}{
		{name: "default", options: option.ProviderURLTestOptions{}, want: "quick"},
		{name: "quick", options: option.ProviderURLTestOptions{Objective: "quick"}, want: "quick"},
		{name: "steady", options: option.ProviderURLTestOptions{Objective: "steady"}, want: "steady"},
		// only the least* objectives limit the pick to max_nodes, the others would use
		// a different node per connection
		{name: "alive", options: option.ProviderURLTestOptions{Objective: "alive"}, wantErr: true},
		{name: "qualified", options: option.ProviderURLTestOptions{Objective: "qualified"}, wantErr: true},
		{name: "unknown", options: option.ProviderURLTestOptions{Objective: "fastest"}, wantErr: true},
		// one tolerance serves both objectives, so it applies whatever the objective and
		// does not have to be tied to one of them
		{
			name:    "tolerance on the default",
			options: option.ProviderURLTestOptions{Tolerance: badoption.Duration(50 * time.Millisecond)},
			want:    "quick",
		},
		{
			name: "tolerance on quick",
			options: option.ProviderURLTestOptions{
				Objective: "quick",
				Tolerance: badoption.Duration(50 * time.Millisecond),
			},
			want: "quick",
		},
		{
			name: "tolerance on steady",
			options: option.ProviderURLTestOptions{
				Objective: "steady",
				Tolerance: badoption.Duration(10 * time.Millisecond),
			},
			want: "steady",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := urlTestObjective(testCase.options)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}
