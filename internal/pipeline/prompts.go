package pipeline

import _ "embed"

// Wrapper — the opening instruction used in every prompt.

//go:embed prompts/prompt_wrapper.txt
var promptWrapper string

// Schemas — JSON output shapes sent to the model in the <schema> block.

//go:embed prompts/team_sync-p2-team_status.schema.json
var teamStatusSchema string

//go:embed prompts/team_sync-p1-sprint_parse.schema.json
var sprintParseSchema string

//go:embed prompts/discovery-homepage_extract.schema.json
var homepageExtractSchema string

//go:embed prompts/team_sync-p2-workload.schema.json
var workloadSchema string

//go:embed prompts/team_sync-p1-velocity.schema.json
var velocitySchema string

//go:embed prompts/org_sync-goal_alignment.schema.json
var alignmentSchema string

//go:embed prompts/discovery-discovery_suggestion.schema.json
var discoverySchema string

//go:embed prompts/autotag-label_match.schema.json
var labelMatchSchema string

// Instructions — natural-language guidance injected as an "instructions" input field.

//go:embed prompts/team_sync-p2-team_status.instructions.txt
var teamStatusInstructions string

//go:embed prompts/discovery-homepage_extract.instructions.txt
var homepageExtractInstructions string

//go:embed prompts/team_sync-p1-sprint_parse.instructions.txt
var sprintParseInstructions string

//go:embed prompts/team_sync-p3-dates_extract.schema.json
var datesExtractSchema string

//go:embed prompts/team_sync-p3-dates_extract.instructions.txt
var datesExtractInstructions string

// Shared context blocks included across multiple pipelines.

//go:embed prompts/sprint-conventions.md
var sprintConventions string
