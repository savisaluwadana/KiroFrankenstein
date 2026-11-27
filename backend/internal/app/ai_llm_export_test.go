package app

import "github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"

// LLMDeciderForTest returns the registered LLM decider of the service, so a
// test can call Decide from many goroutines without the service mutex.
func (s *BranchService) LLMDeciderForTest(sc sim.Scenario) AIDecider { return s.llm.factory(sc) }
