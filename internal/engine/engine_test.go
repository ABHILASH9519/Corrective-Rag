package engine
import("testing")
func TestTokenizationAndScoringPrerequisites(t *testing.T){got:=tokens("Tatkal booking opens at 10 AM for AC classes");if len(got)<4{t.Fatalf("unexpected tokens: %v",got)}}
