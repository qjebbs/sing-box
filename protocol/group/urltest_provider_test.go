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
		// a tolerance belongs to a single objective, so the objective has to be stated
		// for the group to know whether the tolerance applies
		{
			name:    "rtt on the default",
			options: option.ProviderURLTestOptions{RTTTolerance: 0.2},
			wantErr: true,
		},
		{
			name:    "jitter on the default",
			options: option.ProviderURLTestOptions{JitterTolerance: badoption.Duration(10 * time.Millisecond)},
			wantErr: true,
		},
		{
			name: "rtt on quick",
			options: option.ProviderURLTestOptions{
				Objective:    "quick",
				RTTTolerance: 0.2,
			},
			want: "quick",
		},
		{
			name: "jitter on steady",
			options: option.ProviderURLTestOptions{
				Objective:       "steady",
				JitterTolerance: badoption.Duration(10 * time.Millisecond),
			},
			want: "steady",
		},
		{
			name: "jitter on quick",
			options: option.ProviderURLTestOptions{
				Objective:       "quick",
				JitterTolerance: badoption.Duration(10 * time.Millisecond),
			},
			wantErr: true,
		},
		{
			name: "rtt on steady",
			options: option.ProviderURLTestOptions{
				Objective:    "steady",
				RTTTolerance: 0.2,
			},
			wantErr: true,
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
