package protocol

// ACPSkills is the method that lists the skills that a session can load with
// `/skill:<name>`. It takes an ACPSessionRequest and answers ACPSkillsResult.
const ACPSkills = "_ask/session/skills"

// ACPSkillScope tells where a skills folder comes from.
type ACPSkillScope string

const (
	ACPSkillUser    ACPSkillScope = "user"
	ACPSkillProject ACPSkillScope = "project"
)

// ACPSkill is one skill of a session. Path is the absolute path of SKILL.md.
// ModelInvocable is false for a skill that has disable-model-invocation: the
// model never sees it in its list, but `/skill:<name>` still loads it.
type ACPSkill struct {
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	Path           string        `json:"path"`
	Scope          ACPSkillScope `json:"scope"`
	ModelInvocable bool          `json:"modelInvocable"`
}

// ACPSkillsResult lists the skills of a session in the order of the skill list:
// project skills first, then by name and path. It is never null.
type ACPSkillsResult struct {
	Skills []ACPSkill `json:"skills"`
}
