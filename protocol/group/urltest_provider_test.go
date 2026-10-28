package group

import "testing"

func TestURLTestObjective(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "default", value: "", want: "leastping"},
		{name: "leastping", value: "leastping", want: "leastping"},
		{name: "leastload", value: "leastload", want: "leastload"},
		// only the least* objectives limit the pick to max_nodes, the others would use
		// a different node per connection
		{name: "alive", value: "alive", wantErr: true},
		{name: "qualified", value: "qualified", wantErr: true},
		{name: "unknown", value: "fastest", wantErr: true},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := urlTestObjective(testCase.value)
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
