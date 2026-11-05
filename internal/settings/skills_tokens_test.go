package settings

import "testing"

func TestSkillsMaxContextTokens(t *testing.T) {
	tests := []struct {
		name   string
		user   string
		want   int
		source Layer
		bad    bool
	}{
		{"absent", `{}`, 0, LayerDefault, false},
		{"null", `{"skillsMaxContextTokens":null}`, 0, LayerDefault, false},
		{"positive", `{"skillsMaxContextTokens":5000}`, 5000, LayerUser, false},
		{"above the cap is kept", `{"skillsMaxContextTokens":50000}`, 50000, LayerUser, false},
		{"zero", `{"skillsMaxContextTokens":0}`, 0, LayerDefault, true},
		{"negative", `{"skillsMaxContextTokens":-1}`, 0, LayerDefault, true},
		{"fraction", `{"skillsMaxContextTokens":1.5}`, 0, LayerDefault, true},
		{"string", `{"skillsMaxContextTokens":"5000"}`, 0, LayerDefault, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLayeredFixture(t)
			f.writeUser(t, tt.user)

			eff := f.load(t, false)

			wantEqual(t, tt.want, eff.SkillsMaxContextTokens, "SkillsMaxContextTokens")
			wantEqual(t, tt.source, eff.Sources[skillsMaxContextTokensKey], "source")
			if !tt.bad {
				wantEqual(t, 0, len(eff.Diagnostics), "diagnostics")
				return
			}
			wantEqual(t, []Diagnostic{{Layer: LayerUser, Path: f.userPath(), Key: skillsMaxContextTokensKey, Text: "must be a positive integer; it is ignored"}}, eff.Diagnostics, "diagnostics")
		})
	}
}

func TestSkillsMaxContextTokensProjectLayerReplacesUser(t *testing.T) {
	f := newLayeredFixture(t)
	f.writeUser(t, `{"skillsMaxContextTokens":3000}`)
	f.writeProject(t, `{"skillsMaxContextTokens":7000}`, 0644)

	eff := f.load(t, true)

	wantEqual(t, 7000, eff.SkillsMaxContextTokens, "SkillsMaxContextTokens")
	wantEqual(t, LayerProject, eff.Sources[skillsMaxContextTokensKey], "source")

	untrusted := f.load(t, false)
	wantEqual(t, 3000, untrusted.SkillsMaxContextTokens, "an untrusted project file is not read")
}
