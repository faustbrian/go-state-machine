package statemachine

import "testing"

func TestCompileAggregateRejectsEarlyCollectionExcess(t *testing.T) {
	for _, test := range []struct {
		name      string
		allowance int
		change    func(*Definition[string, string, struct{}])
	}{
		{"states", 1, func(*Definition[string, string, struct{}]) {}},
		{"transitions", 2, func(*Definition[string, string, struct{}]) {}},
		{"entry", 3, func(d *Definition[string, string, struct{}]) {
			d.States[0].Entry = []Effect{{Kind: "notification"}}
		}},
		{"exit", 3, func(d *Definition[string, string, struct{}]) {
			d.States[0].Exit = []Effect{{Kind: "notification"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := compileAdmissionDefinition()
			test.change(&definition)
			limits := compileAdmissionLimits()
			limits.MaxCompiledElements = test.allowance
			machine, err := CompileWithLimits(definition, limits)
			if machine != nil {
				t.Fatal("early aggregate excess returned a machine")
			}
			assertDiagnostic(t, err, DiagnosticLimitExceeded)
		})
	}
}
