package core

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wowsims/mop/sim/core/proto"
	"github.com/wowsims/mop/sim/core/simsignals"
)

type Task interface {
	RunTask(sim *Simulation) time.Duration
}

type Simulation struct {
	*Environment

	Options *proto.SimOptions

	rand  Rand
	rseed int64

	// Used for testing only, see RandomFloat().
	isTest    bool
	testRands map[string]Rand

	// Current Simulation State
	pendingActions    []*PendingAction
	pendingActionPool *sync.Pool
	CurrentTime       time.Duration // duration that has elapsed in the sim since starting
	Duration          time.Duration // Duration of current iteration
	NeedsInput        bool          // Sim is in interactive mode and needs input

	ProgressReport func(*proto.ProgressMetrics)
	Signals        simsignals.Signals

	Log func(string, ...interface{})

	executePhase int32 // 20, 25, 35, 45 or 90 for the respective execute range, 100 otherwise

	executePhaseCallbacks []func(*Simulation, int32) // 2nd parameter is 90 for 90%, 45 for 45%, 35 for 35%, 25 for 25% and 20 for 20%

	nextExecuteDuration time.Duration
	nextExecuteDamage   float64

	endOfCombatDuration time.Duration
	endOfCombatDamage   float64

	minTrackerTime time.Duration
	trackers       []*auraTracker

	minWeaponAttackTime time.Duration
	weaponAttacks       []*WeaponAttack

	minTaskTime time.Duration
	tasks       []Task

	isInPrepull bool
}

func (sim *Simulation) rescheduleTracker(trackerTime time.Duration) {
	sim.minTrackerTime = min(sim.minTrackerTime, trackerTime)
}

func (sim *Simulation) addTracker(tracker *auraTracker) {
	sim.trackers = append(sim.trackers, tracker)
	sim.rescheduleTracker(tracker.minExpires)
}

func (sim *Simulation) removeTracker(tracker *auraTracker) {
	if idx := slices.Index(sim.trackers, tracker); idx != -1 {
		sim.trackers = removeBySwappingToBack(sim.trackers, idx)
	}
}

func (sim *Simulation) rescheduleWeaponAttack(weaponAttackTime time.Duration) {
	sim.minWeaponAttackTime = min(sim.minWeaponAttackTime, weaponAttackTime)
}

func (sim *Simulation) addWeaponAttack(weaponAttack *WeaponAttack) {
	sim.weaponAttacks = append(sim.weaponAttacks, weaponAttack)
}

func (sim *Simulation) removeWeaponAttack(weaponAttack *WeaponAttack) {
	if idx := slices.Index(sim.weaponAttacks, weaponAttack); idx != -1 {
		sim.weaponAttacks = removeBySwappingToBack(sim.weaponAttacks, idx)
	}
}

func (sim *Simulation) RescheduleTask(taskTime time.Duration) {
	sim.minTaskTime = min(sim.minTaskTime, taskTime)
}

func (sim *Simulation) AddTask(task Task) {
	sim.tasks = append(sim.tasks, task)
}

func (sim *Simulation) RemoveTask(task Task) {
	if idx := slices.Index(sim.tasks, task); idx != -1 {
		sim.tasks = removeBySwappingToBack(sim.tasks, idx)
	}
}

func RunSim(rsr *proto.RaidSimRequest, progress chan *proto.ProgressMetrics, signals simsignals.Signals) *proto.RaidSimResult {
	return runSim(rsr, progress, false, signals)
}

func runSim(rsr *proto.RaidSimRequest, progress chan *proto.ProgressMetrics, skipPresim bool, signals simsignals.Signals) (result *proto.RaidSimResult) {
	if !rsr.SimOptions.IsTest {
		defer func() {
			if err := recover(); err != nil {
				errStr := ""
				switch errt := err.(type) {
				case string:
					errStr = errt
				case error:
					errStr = errt.Error()
				}

				errStr += "\nStack Trace:\n" + string(debug.Stack())
				result = &proto.RaidSimResult{
					Error: &proto.ErrorOutcome{Message: errStr},
				}
				if progress != nil {
					progress <- &proto.ProgressMetrics{
						FinalRaidResult: result,
					}
				}
			}
			if progress != nil {
				close(progress)
			}
		}()
	}

	sim := NewSim(rsr, signals)

	if !skipPresim {
		if progress != nil {
			progress <- &proto.ProgressMetrics{
				TotalIterations: sim.Options.Iterations,
				PresimRunning:   true,
			}
			runtime.Gosched() // allow time for message to make it back out.
		}
		presimResult := sim.runPresims(rsr)
		if presimResult != nil && presimResult.Error != nil {
			if progress != nil {
				progress <- &proto.ProgressMetrics{
					TotalIterations: sim.Options.Iterations,
					FinalRaidResult: presimResult,
				}
			}
			return presimResult
		}
		if progress != nil {
			progress <- &proto.ProgressMetrics{
				TotalIterations: sim.Options.Iterations,
				PresimRunning:   false,
			}
			sim.ProgressReport = func(progMetric *proto.ProgressMetrics) {
				progress <- progMetric
			}
			runtime.Gosched() // allow time for message to make it back out.
		}
		// Use pre-sim as estimate for length of fight (when using health fight)
		if sim.Encounter.EndFightAtHealth > 0 && presimResult != nil {
			sim.BaseDuration = time.Duration(presimResult.AvgIterationDuration) * time.Second
			sim.Duration = time.Duration(presimResult.AvgIterationDuration) * time.Second
			sim.Encounter.DurationIsEstimate = false // we now have a pretty good value for duration
		}
	}

	// using a variable here allows us to mutate it in the deferred recover, sending out error info
	result = sim.run()

	return result
}

func NewSim(rsr *proto.RaidSimRequest, signals simsignals.Signals) *Simulation {
	env, _, _ := NewEnvironment(rsr.Raid, rsr.Encounter, false)
	return newSimWithEnv(env, rsr.SimOptions, signals)
}

func newSimWithEnv(env *Environment, simOptions *proto.SimOptions, signals simsignals.Signals) *Simulation {
	rseed := simOptions.RandomSeed
	if rseed == 0 {
		rseed = time.Now().UnixNano()
	}

	return &Simulation{
		Environment: env,
		Options:     simOptions,

		rand:  NewSplitMix(uint64(rseed)),
		rseed: rseed,

		isTest:    simOptions.IsTest || simOptions.UseLabeledRands,
		testRands: make(map[string]Rand),

		Signals: signals,

		pendingActionPool: &sync.Pool{
			New: func() any {
				return &PendingAction{
					canPool: true,
				}
			},
		},
	}
}

// Returns a random float64 between 0.0 (inclusive) and 1.0 (exclusive).
//
// In tests, although we can set the initial seed, test results are still very
// sensitive to the exact order of RandomFloat() calls. To mitigate this, when
// testing we use a separate rand object for each RandomFloat callsite,
// distinguished by the label string.
func (sim *Simulation) RandomFloat(label string) float64 {
	return sim.labelRand(label).NextFloat64()
}

func (sim *Simulation) labelRand(label string) Rand {
	if !sim.isTest {
		return sim.rand
	}

	labelRng, ok := sim.testRands[label]
	if !ok {
		// Add rseed to the label, so we still have run-run variance for stat weights.
		labelRng = NewSplitMix(uint64(makeTestRandSeed(sim.rand.GetSeed(), label)))
		sim.testRands[label] = labelRng
	}
	return labelRng
}

func (sim *Simulation) reseedRands(i int64) {
	rseed := sim.Options.RandomSeed + i
	sim.rand.Seed(rseed)

	if sim.isTest {
		for label, rng := range sim.testRands {
			rng.Seed(makeTestRandSeed(rseed, label))
		}
	}
}

func makeTestRandSeed(rseed int64, label string) int64 {
	return int64(hash(label + strconv.FormatInt(rseed, 16)))
}

func (sim *Simulation) RandomExpFloat(label string) float64 {
	return rand.New(sim.labelRand(label)).ExpFloat64()
}

// Shorthand for commonly-used RNG behavior.
// Returns a random number between min and max.
func (sim *Simulation) Roll(min float64, max float64) float64 {
	return sim.RollWithLabel(min, max, "Damage Roll")
}
func (sim *Simulation) RollWithLabel(min float64, max float64, label string) float64 {
	return min + (max-min)*sim.RandomFloat(label)
}

func (sim *Simulation) Proc(p float64, label string) bool {
	switch {
	case p >= 1:
		return true
	case p <= 0:
		return false
	default:
		return sim.RandomFloat(label) < p
	}
}

func (sim *Simulation) Reset() {
	sim.reset()
}

func (sim *Simulation) Reseed(seed int64) {
	sim.reseedRands(seed)
}

// Run runs the simulation for the configured number of iterations, and
// collects all the metrics together.
func (sim *Simulation) run() *proto.RaidSimResult {
	t0 := time.Now()

	logsBuffer := &strings.Builder{}
	if sim.Options.Debug || sim.Options.DebugFirstIteration {
		sim.Log = func(message string, vals ...interface{}) {
			logsBuffer.WriteString(fmt.Sprintf("[%0.2f] "+message+"\n", append([]interface{}{sim.CurrentTime.Seconds()}, vals...)...))
		}
	}

	// Uncomment this to print logs directly to console.
	// sim.Options.Debug = true
	// sim.Log = func(message string, vals ...interface{}) {
	// 	fmt.Printf(fmt.Sprintf("[%0.1f] "+message+"\n", append([]interface{}{sim.CurrentTime.Seconds()}, vals...)...))
	// }

	sim.runOnce()
	firstIterationDuration := sim.Duration
	if sim.Encounter.EndFightAtHealth != 0 {
		firstIterationDuration = sim.CurrentTime
	}
	totalDuration := firstIterationDuration

	if !sim.Options.Debug {
		sim.Log = nil
	}

	var st time.Time
	for i := int32(1); i < sim.Options.Iterations; i++ {
		if sim.Signals.Abort.IsTriggered() {
			quitResult := &proto.RaidSimResult{Error: &proto.ErrorOutcome{Type: proto.ErrorOutcomeType_ErrorOutcomeAborted}}
			if sim.ProgressReport != nil {
				sim.ProgressReport(&proto.ProgressMetrics{FinalRaidResult: quitResult})
			}
			return quitResult
		}

		// fmt.Printf("Iteration: %d\n", i)
		if sim.ProgressReport != nil && time.Since(st) > time.Millisecond*100 {
			metrics := sim.Raid.GetMetrics()
			sim.ProgressReport(&proto.ProgressMetrics{TotalIterations: sim.Options.Iterations, CompletedIterations: i, Dps: metrics.Dps.Avg, Hps: metrics.Hps.Avg})
			if IsRunningInWasm() {
				time.Sleep(time.Microsecond) // Need to sleep to escape the go scheduler in wasm to give the JS event loop a chance to process requests to the worker.
			} else {
				runtime.Gosched() // ensure that reporting threads are given time to report, mostly only important in wasm (only 1 thread)
			}
			st = time.Now()
		}

		// Before each iteration, reset state to seed+iterations
		sim.reseedRands(int64(i))

		sim.runOnce()
		iterDuration := sim.Duration
		if sim.Encounter.EndFightAtHealth != 0 {
			iterDuration = sim.CurrentTime
		}
		totalDuration += iterDuration
	}
	result := &proto.RaidSimResult{
		RaidMetrics:      sim.Raid.GetMetrics(),
		EncounterMetrics: sim.Encounter.GetMetricsProto(),

		Logs:                   logsBuffer.String(),
		FirstIterationDuration: firstIterationDuration.Seconds(),
		AvgIterationDuration:   totalDuration.Seconds() / float64(sim.Options.Iterations),
		IterationsDone:         sim.Options.Iterations,
	}

	// Final progress report
	if sim.ProgressReport != nil {
		sim.ProgressReport(&proto.ProgressMetrics{TotalIterations: sim.Options.Iterations, CompletedIterations: sim.Options.Iterations, Dps: result.RaidMetrics.Dps.Avg, FinalRaidResult: result})
	}

	if d := sim.Options.Iterations; d > 3000 {
		log.Printf("running %d iterations took %s", d, time.Since(t0))
	}

	return result
}

// RunOnce is the main event loop. It will run the simulation for number of seconds.
func (sim *Simulation) runOnce() {
	sim.isInPrepull = true
	sim.reset()
	sim.PrePull()
	sim.isInPrepull = false
	sim.runPendingActions()
	sim.Cleanup()
}

var (
	sentinelPendingAction = &PendingAction{
		NextActionAt: NeverExpires,
		OnAction: func(sim *Simulation) {
			panic("running sentinel pending action")
		},
	}
)

// Reset will set sim back and erase all current state.
// This is automatically called before every 'Run'.
func (sim *Simulation) reset() {
	if sim.Encounter.DurationIsEstimate && sim.CurrentTime != 0 {
		sim.BaseDuration = sim.CurrentTime
		sim.Encounter.DurationIsEstimate = false
	}
	sim.Duration = sim.BaseDuration
	if sim.DurationVariation != 0 {
		variation := sim.DurationVariation * 2
		sim.Duration += time.Duration(sim.RandomFloat("sim duration")*float64(variation)) - sim.DurationVariation
	}

	sim.pendingActions = sim.pendingActions[:0]
	sim.pendingActions = append(sim.pendingActions, sentinelPendingAction)

	sim.executePhase = 0
	sim.nextExecutePhase()
	sim.executePhaseCallbacks = nil

	// Use duration as an end check if not using health.
	sim.endOfCombatDuration = sim.Duration
	sim.endOfCombatDamage = math.MaxFloat64
	if sim.Encounter.EndFightAtHealth > 0 {
		sim.endOfCombatDuration = NeverExpires
		sim.endOfCombatDamage = sim.Encounter.EndFightAtHealth
	}

	sim.CurrentTime = 0

	sim.trackers = sim.trackers[:0]
	sim.minTrackerTime = NeverExpires

	sim.weaponAttacks = sim.weaponAttacks[:0]
	sim.minWeaponAttackTime = NeverExpires

	sim.tasks = sim.tasks[:0]
	sim.minTaskTime = NeverExpires

	sim.Environment.reset(sim)

	sim.initManaTickAction()
}

func (sim *Simulation) PrePull() {
	if len(sim.prepullActions) > 0 {
		sim.CurrentTime = sim.prepullActions[0].DoAt

		for i, ppa := range sim.prepullActions {
			sim.AddPendingAction(&PendingAction{
				NextActionAt: ppa.DoAt,
				Priority:     ActionPriorityPrePull + ActionPriority(len(sim.prepullActions)-i),
				OnAction:     ppa.Action,
			})
		}
	}

	sim.AddPendingAction(&PendingAction{
		NextActionAt: 0,
		Priority:     ActionPriorityPrePull + ActionPriority(len(sim.prepullActions)+1),
		OnAction: func(sim *Simulation) {
			for _, unit := range sim.Environment.AllUnits {
				if unit.enabled {
					unit.onEncounterStart(sim)
				}
			}

			for _, unit := range sim.Environment.AllUnits {
				if unit.enabled {
					unit.startPull(sim)
				}
			}
		},
	})
}

func (sim *Simulation) Cleanup() {
	// The last event loop will leave CurrentTime at some value close to but not
	// quite at the Duration. Explicitly set this so that accesses to CurrentTime
	// during the doneIteration phase will return the Duration value, which is
	// intuitive. When using Health sims use the actual end time
	if sim.Encounter.EndFightAtHealth == 0 {
		sim.CurrentTime = sim.Duration
	} else {
		sim.Duration = sim.CurrentTime
	}

	for _, pa := range sim.pendingActions {
		if pa.CleanUp != nil {
			pa.CleanUp(sim)
		}

		pa.dispose(sim)
	}

	sim.Raid.doneIteration(sim)
	sim.Encounter.doneIteration(sim)

	for _, unit := range sim.Raid.AllUnits {
		unit.Metrics.doneIteration(unit, sim)
	}
	for _, target := range sim.Encounter.AllTargetUnits {
		target.Metrics.doneIteration(target, sim)
	}
}

func (sim *Simulation) runPendingActions() {
	for {
		if finished := sim.Step(); finished {
			return
		}
	}
}

func (sim *Simulation) Step() bool {
	last := len(sim.pendingActions) - 1
	pa := sim.pendingActions[last]

	if pa.NextActionAt >= sim.minWeaponAttackTime && sim.minWeaponAttackTime <= sim.minTaskTime {
		if sim.minWeaponAttackTime > sim.endOfCombatDuration || sim.Encounter.DamageTaken > sim.endOfCombatDamage {
			return true
		}
		sim.advanceWeaponAttacks()
		return false
	}

	if pa.NextActionAt >= sim.minTaskTime {
		if sim.minTaskTime > sim.endOfCombatDuration || sim.Encounter.DamageTaken > sim.endOfCombatDamage {
			return true
		}
		sim.advanceTasks()
		return false
	}

	sim.pendingActions = sim.pendingActions[:last]
	pa.consumed = true

	if pa.cancelled {
		pa.dispose(sim)
		return false
	}

	if pa.NextActionAt > sim.endOfCombatDuration || sim.Encounter.DamageTaken > sim.endOfCombatDamage {
		pa.dispose(sim)
		return true
	}

	if pa.NextActionAt > sim.CurrentTime {
		sim.advance(pa.NextActionAt)
	}

	if pa.cancelled {
		pa.dispose(sim)
		return false
	}

	pa.OnAction(sim)
	pa.dispose(sim)
	return false
}

func (sim *Simulation) advanceWeaponAttacks() {
	if sim.minWeaponAttackTime > sim.CurrentTime {
		sim.advance(sim.minWeaponAttackTime)
	}

	sim.minWeaponAttackTime = NeverExpires
	for _, wa := range sim.weaponAttacks {
		sim.minWeaponAttackTime = min(sim.minWeaponAttackTime, wa.trySwing(sim))
	}
}

func (sim *Simulation) advanceTasks() {
	if sim.minTaskTime > sim.CurrentTime {
		sim.advance(sim.minTaskTime)
	}

	sim.minTaskTime = NeverExpires
	for _, t := range sim.tasks {
		sim.minTaskTime = min(sim.minTaskTime, t.RunTask(sim)) // RunTask() might alter sim.tasks
	}
}

// Advance moves time forward counting down auras, CDs, mana regen, etc
func (sim *Simulation) advance(nextTime time.Duration) {
	sim.CurrentTime = nextTime

	// this is a loop to handle duplicate ExecuteProportions, e.g. if they're all set to 100%, you reach
	// execute phases 90%, 45%, 35%, 25%, and 20% in the first advance() call.
	for sim.CurrentTime >= sim.nextExecuteDuration || sim.Encounter.DamageTaken >= sim.nextExecuteDamage {
		sim.nextExecutePhase()
		for _, callback := range sim.executePhaseCallbacks {
			callback(sim, sim.executePhase)
		}
	}

	if sim.CurrentTime >= sim.minTrackerTime {
		sim.minTrackerTime = NeverExpires
		for _, t := range sim.trackers {
			sim.minTrackerTime = min(sim.minTrackerTime, t.tryAdvance(sim))
		}
	}
}
func (sim *Simulation) setupReverseExecute(phase int32, activeProportion float64) {
	sim.executePhase = phase
	// This calculates the duration for which the phase is active from the start
	sim.nextExecuteDuration = time.Duration(activeProportion * float64(sim.Duration))
}

// nextExecutePhase updates nextExecuteDuration and nextExecuteDamage based on executePhase.
func (sim *Simulation) nextExecutePhase() {
	setup := func(phase int32, damage float64, health float64) {
		sim.executePhase = phase
		if sim.Encounter.EndFightAtHealth > 0 {
			sim.nextExecuteDamage = (1 - damage) * sim.Encounter.EndFightAtHealth
		} else {
			sim.nextExecuteDuration = time.Duration((1 - health) * float64(sim.Duration))
		}
	}

	sim.nextExecuteDuration = NeverExpires
	sim.nextExecuteDamage = math.MaxFloat64

	switch sim.executePhase {
	case 0: // initially waiting for 90%
		setup(100, 0.90, sim.Encounter.ExecuteProportion_90)
	case 100: // at 90%, waiting for 45%
		setup(90, 0.45, sim.Encounter.ExecuteProportion_45)
	case 90: // at 45%, waiting for 35%
		setup(45, 0.35, sim.Encounter.ExecuteProportion_35)
	case 45: // at 35%, waiting for 25%
		setup(35, 0.25, sim.Encounter.ExecuteProportion_25)
	case 35: // at 25%, waiting for 20%
		setup(25, 0.20, sim.Encounter.ExecuteProportion_20)
	case 25: // at 20%, done waiting
		sim.executePhase = 20 // could also be used for end of fight handling
	default:
		panic(fmt.Sprintf("executePhase = %d invalid", sim.executePhase))
	}
}

func (sim *Simulation) AddPendingAction(pa *PendingAction) {
	//if pa.NextActionAt < sim.CurrentTime {
	//	panic(fmt.Sprintf("Cant add action in the past: %s", pa.NextActionAt))
	//}
	pa.consumed = false
	for index, v := range sim.pendingActions[1:] {
		if v.NextActionAt < pa.NextActionAt || (v.NextActionAt == pa.NextActionAt && v.Priority >= pa.Priority) {
			//if sim.Log != nil {
			//	sim.Log("Adding action at index %d for time %s", index - len(sim.pendingActions), pa.NextActionAt)
			//	for i := index; i < len(sim.pendingActions); i++ {
			//		sim.Log("Upcoming action at %s", sim.pendingActions[i].NextActionAt)
			//	}
			//}
			sim.pendingActions = append(sim.pendingActions, pa)
			copy(sim.pendingActions[index+2:], sim.pendingActions[index+1:])
			sim.pendingActions[index+1] = pa
			return
		}
	}
	//if sim.Log != nil {
	//	sim.Log("Adding action at end for time %s", pa.NextActionAt)
	//}
	sim.pendingActions = append(sim.pendingActions, pa)
}

// Use this for any "fire and forget" delayed actions where your code does not
// require persistent access to the returned *PendingAction pointer. This helper
// avoids unnecessary re-allocations of the PendingAction struct for improved
// code performance, but offers no guarantees on the long-term state of the
// underlying struct, which will be re-used once consumed.
func (sim *Simulation) GetConsumedPendingActionFromPool() *PendingAction {
	pa := sim.pendingActionPool.Get().(*PendingAction)
	pa.NextActionAt = 0
	pa.Priority = 0
	pa.OnAction = nil
	pa.CleanUp = nil
	pa.cancelled = false
	return pa
}

func (sim *Simulation) RegisterExecutePhaseCallback(callback func(sim *Simulation, isExecute int32)) {
	sim.executePhaseCallbacks = append(sim.executePhaseCallbacks, callback)
}
func (sim *Simulation) IsExecutePhase20() bool {
	return sim.executePhase <= 20
}
func (sim *Simulation) IsExecutePhase25() bool {
	return sim.executePhase <= 25
}
func (sim *Simulation) IsExecutePhase35() bool {
	return sim.executePhase <= 35
}
func (sim *Simulation) IsExecutePhase45() bool {
	return sim.executePhase <= 45
}
func (sim *Simulation) IsExecutePhase90() bool {
	return sim.executePhase > 90
}

func (sim *Simulation) GetRemainingDuration() time.Duration {
	if sim.Encounter.EndFightAtHealth > 0 {
		if !sim.Encounter.DurationIsEstimate || sim.CurrentTime < time.Second*5 {
			return sim.Duration - sim.CurrentTime
		}

		// Estimate time remaining via avg dps
		dps := sim.Encounter.DamageTaken / sim.CurrentTime.Seconds()
		dur := time.Duration((sim.Encounter.EndFightAtHealth-sim.Encounter.DamageTaken)/dps) * time.Second
		return dur
	}
	return sim.Duration - sim.CurrentTime
}

// Returns the percentage of time remaining in the current iteration, as a value from 0-1.
func (sim *Simulation) GetRemainingDurationPercent() float64 {
	if sim.Encounter.EndFightAtHealth > 0 {
		return 1.0 - sim.Encounter.DamageTaken/sim.Encounter.EndFightAtHealth
	}
	return float64(sim.Duration-sim.CurrentTime) / float64(sim.Duration)
}
