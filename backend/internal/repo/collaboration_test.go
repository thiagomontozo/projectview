package repo

import (
	"reflect"
	"testing"
)

func TestExtractMentions(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"a single mention", "hey @ana can you look?", []string{"ana"}},
		{"several mentions", "@ana and @bruno please review", []string{"ana", "bruno"}},
		{"mention at the start", "@ana hello", []string{"ana"}},
		{"case is normalised", "@Ana @ANA", []string{"ana"}},
		{"dots, dashes and underscores are part of a username",
			"@ana.paula @jean-luc @some_bot", []string{"ana.paula", "jean-luc", "some_bot"}},

		// An e-mail address must not be read as a mention: the @ is preceded
		// by a word character, which the pattern requires not to be.
		{"an e-mail address is not a mention", "write to ana@example.com", nil},
		{"an e-mail among text", "contact bruno@corp.io about it", nil},

		// Guards against the pattern matching a bare @ or a one-letter name,
		// which would fire on ordinary punctuation.
		{"a bare at sign", "meet @ 5pm", nil},
		{"a one-character name is too short", "@a", nil},

		{"no mentions at all", "just a normal message", nil},
		{"empty body", "", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractMentions(c.body)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ExtractMentions(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestExtractMentionsDeduplicates(t *testing.T) {
	got := ExtractMentions("@ana @ana @ana")
	if len(got) != 1 || got[0] != "ana" {
		t.Errorf("ExtractMentions = %v, want a single entry; naming someone three times is one mention", got)
	}
}

func TestValidDigest(t *testing.T) {
	for _, valid := range []string{"off", "daily", "weekly"} {
		if !ValidDigest(valid) {
			t.Errorf("%q should be a valid digest setting", valid)
		}
	}
	for _, invalid := range []string{"", "hourly", "DAILY", "monthly"} {
		if ValidDigest(invalid) {
			t.Errorf("%q should not be a valid digest setting", invalid)
		}
	}
}
