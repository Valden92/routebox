package api_test

import (
	"testing"

	"github.com/Valden92/routebox/internal/api"
)

func TestPersonalVPNTiedToSubscription(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		activeID  string
		deletedID string
		want      bool
	}{
		{name: "delete other while vpn on active", activeID: "dary", deletedID: "other", want: false},
		{name: "delete active", activeID: "dary", deletedID: "dary", want: true},
		{name: "empty active", activeID: "", deletedID: "other", want: false},
		{name: "empty deleted", activeID: "dary", deletedID: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := api.PersonalVPNTiedToSubscription(tc.activeID, tc.deletedID)
			if got != tc.want {
				t.Fatalf("PersonalVPNTiedToSubscription(%q,%q)=%v want %v", tc.activeID, tc.deletedID, got, tc.want)
			}
		})
	}
}
