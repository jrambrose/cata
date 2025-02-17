package feral

import (
	"math"
	"time"

	"github.com/wowsims/cata/sim/core"
	"github.com/wowsims/cata/sim/core/proto"
	"github.com/wowsims/cata/sim/druid"
)

func (cat *FeralDruid) OnGCDReady(sim *core.Simulation) {
	if !cat.usingHardcodedAPL {
		return
	}

	if !cat.GCD.IsReady(sim) {
		return
	}

	cat.bleedAura = cat.CurrentTarget.GetExclusiveEffectCategory(core.BleedEffectCategory).GetActiveAura()

	if cat.preRotationCleanup(sim) {
		valid := false
		nextAction := time.Duration(0)
		if cat.Rotation.RotationType == proto.FeralDruid_Rotation_SingleTarget {
			valid, nextAction = cat.doRotation(sim)
		} else {
			valid, nextAction = cat.doAoeRotation(sim)
		}
		if valid {
			cat.postRotation(sim, nextAction)
		}
	}
}

func (cat *FeralDruid) NextRotationAction(sim *core.Simulation, kickAt time.Duration) {
	if cat.rotationAction != nil {
		cat.rotationAction.Cancel(sim)
	}

	cat.rotationAction = &core.PendingAction{
		Priority:     core.ActionPriorityGCD,
		OnAction:     cat.OnGCDReady,
		NextActionAt: kickAt,
	}

	sim.AddPendingAction(cat.rotationAction)
}

func (cat *FeralDruid) checkReplaceMaul(sim *core.Simulation, mhSwingSpell *core.Spell) *core.Spell {
	return mhSwingSpell
}

func (cat *FeralDruid) shiftBearCat(sim *core.Simulation, powershift bool) bool {
	cat.waitingForTick = false

	// If we have just now decided to shift, then we do not execute the
	// shift immediately, but instead trigger an input delay for realism.
	if !cat.readyToShift {
		cat.readyToShift = true
		return false
	}
	cat.readyToShift = false

	toCat := !cat.InForm(druid.Cat)
	if powershift {
		toCat = !toCat
	}

	cat.lastShift = sim.CurrentTime
	if toCat {
		return cat.CatForm.Cast(sim, nil)
	} else {
		cat.BearForm.Cast(sim, nil)
		// Bundle Enrage if available
		if cat.Enrage.IsReady(sim) {
			cat.Enrage.Cast(sim, nil)
		}
		return true
	}
}

func (cat *FeralDruid) canBite(sim *core.Simulation, isExecutePhase bool) bool {
	if cat.SavageRoarAura.RemainingDuration(sim) < cat.Rotation.BiteTime {
		return false
	}

	if isExecutePhase {
		return true
	}

	return cat.Rip.CurDot().RemainingDuration(sim) >= cat.Rotation.BiteTime
}

func (cat *FeralDruid) berserkExpectedAt(sim *core.Simulation, futureTime time.Duration) bool {
	if cat.BerserkAura.IsActive() {
		return futureTime < cat.BerserkAura.ExpiresAt() || futureTime > cat.Berserk.ReadyAt()
	}
	if cat.Berserk.IsReady(sim) {
		return futureTime > sim.CurrentTime+cat.Berserk.CD.Duration
	}
	if cat.TigersFuryAura.IsActive() && cat.Talents.Berserk {
		return futureTime > cat.TigersFuryAura.ExpiresAt()
	}
	return false
}

func (cat *FeralDruid) calcBuilderDpe(sim *core.Simulation) (float64, float64) {
	// Calculate current damage-per-Energy of Rake vs. Shred. Used to
	// determine whether Rake is worth casting when player stats change upon a
	// dynamic proc occurring
	shredDpc := cat.Shred.ExpectedInitialDamage(sim, cat.CurrentTarget)
	potentialRakeTicks := min(cat.Rake.CurDot().NumberOfTicks, int32(sim.GetRemainingDuration()/time.Second*3))
	rakeDpc := cat.Rake.ExpectedInitialDamage(sim, cat.CurrentTarget) + cat.Rake.ExpectedTickDamage(sim, cat.CurrentTarget)*float64(potentialRakeTicks)
	return rakeDpc / cat.Rake.DefaultCast.Cost, shredDpc / cat.Shred.DefaultCast.Cost
}

func (cat *FeralDruid) clipRoar(sim *core.Simulation, isExecutePhase bool) bool {
	ripDot := cat.Rip.CurDot()
	ripdotRemaining := ripDot.RemainingDuration(sim)
	simTimeRemaining := sim.GetRemainingDuration()

	if !ripDot.IsActive() || (simTimeRemaining-ripdotRemaining < 10*time.Second) {
		return false
	}

	// Project Rip end time assuming full Glyph of Shred extensions
	remainingExtensions := cat.maxRipTicks - ripDot.NumberOfTicks
	ripDur := ripdotRemaining + time.Duration(remainingExtensions) * ripDot.TickLength
	roarDur := cat.SavageRoarAura.RemainingDuration(sim)

	if roarDur > (ripDur + cat.Rotation.RipLeeway) {
		return false
	}

	if roarDur >= simTimeRemaining {
		return false
	}

	// Calculate when roar would end if casted now
	newRoarDur := cat.SavageRoarDurationTable[cat.ComboPoints()]

	// If a fresh Roar cast now would cover us to end of fight, then clip now for maximum CP efficiency.
	if newRoarDur >= simTimeRemaining {
		return true
	}

	// Clip as soon as we have enough CPs for the new roar to expire well
	// after the current rip
	if !isExecutePhase {
		return newRoarDur >= (ripDur + cat.Rotation.MinRoarOffset)
	}

	// Under Execute conditions, ignore the offset rule and instead optimize for as few Roar casts as possible.
	if cat.ComboPoints() < 5 {
		return false
	}

	minRoarsPossible := (simTimeRemaining - roarDur) / newRoarDur
	projectedRoarCasts := simTimeRemaining / newRoarDur
	return projectedRoarCasts == minRoarsPossible
}

func (cat *FeralDruid) tfExpectedBefore(sim *core.Simulation, futureTime time.Duration) bool {
	if !cat.TigersFury.IsReady(sim) {
		return cat.TigersFury.ReadyAt() < futureTime
	}
	if cat.BerserkAura.IsActive() {
		return cat.BerserkAura.ExpiresAt() < futureTime
	}
	return true
}

func (cat *FeralDruid) TryTigersFury(sim *core.Simulation) {
	// Handle tigers fury
	if !cat.TigersFury.IsReady(sim) {
		return
	}

	gcdTimeToRdy := cat.GCD.TimeToReady(sim)
	leewayTime := max(gcdTimeToRdy, cat.ReactionTime)
	tfEnergyThresh := 40.0 - 10.0*(leewayTime+core.Ternary(cat.ClearcastingAura.IsActive(), 1*time.Second, 0)).Seconds()
	tfNow := (cat.CurrentEnergy() < tfEnergyThresh) && !cat.BerserkAura.IsActive()

	// If Lacerateweaving, then delay Tiger's Fury if Lacerate is due to
	// expire within 3 GCDs (two cat specials + shapeshift), since we
	// won't be able to spend down our Energy fast enough to avoid
	// Energy capping otherwise.
	lacerateDot := cat.Lacerate.CurDot()
	if cat.Rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate {
		nextPossibleLac := sim.CurrentTime + leewayTime + cat.ReactionTime + time.Duration(3.5*float64(time.Second))
		tfNow = tfNow && (!lacerateDot.IsActive() || (lacerateDot.ExpiresAt() > nextPossibleLac) || (lacerateDot.RemainingDuration(sim) > sim.GetRemainingDuration()))
	}

	if tfNow {
		cat.TigersFury.Cast(sim, nil)
		// Kick gcd loop, also need to account for any gcd 'left'
		// otherwise it breaks gcd logic
		cat.NextRotationAction(sim, sim.CurrentTime+leewayTime)
	}
}

func (cat *FeralDruid) TryBerserk(sim *core.Simulation) {
	// Berserk algorithm: time Berserk for just after a Tiger's Fury
	// *unless* we'll lose Berserk uptime by waiting for Tiger's Fury to
	// come off cooldown. The latter exception is necessary for
	// Lacerateweave rotation since TF timings can drift over time.
	simTimeRemain := sim.GetRemainingDuration()
	waitForTf := cat.Talents.Berserk && (cat.TigersFury.ReadyAt() <= cat.BerserkAura.Duration) && (cat.TigersFury.ReadyAt()+cat.ReactionTime < simTimeRemain-cat.BerserkAura.Duration)
	isClearcast := cat.ClearcastingAura.IsActive()
	berserkNow := cat.Berserk.IsReady(sim) && !waitForTf && !isClearcast

	// Additionally, for Lacerateweave rotation, postpone the final Berserk
	// of the fight to as late as possible so as to minimize the impact of
	// dropping Lacerate stacks during the Berserk window. Rationale for the
	// 3 second additional leeway given beyond just berserk_dur in the below
	// expression is to be able to fit in a final TF and dump the Energy
	// from it in cases where Berserk and TF CDs are desynced due to drift.
	if berserkNow && cat.Rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && cat.berserkUsed && simTimeRemain < cat.Berserk.CD.Duration {
		berserkNow = simTimeRemain < cat.BerserkAura.Duration+(3*time.Second)
	}

	if berserkNow {
		cat.Berserk.Cast(sim, nil)
		cat.UpdateMajorCooldowns()

		// Kick gcd loop, also need to account for any gcd 'left'
		// otherwise it breaks gcd logic
		gcdTimeToRdy := cat.GCD.TimeToReady(sim)
		leewayTime := max(gcdTimeToRdy, cat.ReactionTime)
		cat.NextRotationAction(sim, sim.CurrentTime+leewayTime)
	}
}

func (cat *FeralDruid) preRotationCleanup(sim *core.Simulation) bool {
	if cat.BerserkAura.IsActive() {
		cat.berserkUsed = true
	}

	// If we previously decided to shift, then execute the shift now once
	// the input delay is over.
	if cat.readyToShift {
		cat.shiftBearCat(sim, false)
		// Reset swing timer from snek (or idol/weapon swap) when going into cat
		if cat.InForm(druid.Cat) && cat.Rotation.SnekWeave {
			cat.AutoAttacks.StopMeleeUntil(sim, sim.CurrentTime, false)
		}
		return false
	}

	return true
}

func (cat *FeralDruid) postRotation(sim *core.Simulation, nextAction time.Duration) {
	// Also schedule an action right at Energy cap to make sure we never
	// accidentally over-cap while waiting on other timers.
	timeToCap := core.DurationFromSeconds((cat.MaximumEnergy() - cat.CurrentEnergy()) / cat.EnergyRegenPerSecond())
	nextAction = min(nextAction, sim.CurrentTime+timeToCap)

	nextAction += cat.ReactionTime

	if nextAction <= sim.CurrentTime {
		panic("nextaction in the past")
	} else {
		cat.NextRotationAction(sim, nextAction)
	}
}

func (cat *FeralDruid) doRotation(sim *core.Simulation) (bool, time.Duration) {
	// Store state variables for re-use
	rotation := &cat.Rotation
	curEnergy := cat.CurrentEnergy()
	curRage := cat.CurrentRage()
	curCp := cat.ComboPoints()
	isClearcast := cat.ClearcastingAura.IsActive()
	simTimeRemain := sim.GetRemainingDuration()
	shiftCost := cat.CatForm.DefaultCast.Cost
	rakeDot := cat.Rake.CurDot()
	ripDot := cat.Rip.CurDot()
	lacerateDot := cat.Lacerate.CurDot()
	isBleedActive := cat.AssumeBleedActive || ripDot.IsActive() || rakeDot.IsActive() || lacerateDot.IsActive()
	regenRate := cat.EnergyRegenPerSecond()
	isExecutePhase := rotation.BiteDuringExecute && sim.IsExecutePhase25()

	// Prioritize using rake/rip with omen procs if bleed isnt active
	// But less priority then mangle aura
	ripCcCheck := core.Ternary(isBleedActive, !isClearcast, true)
	rakeCcCheck := core.Ternary(isBleedActive, !isClearcast, cat.bleedAura.IsActive())

	endThresh := time.Second * 10

	ripNow := (curCp >= rotation.MinCombosForRip) && (!ripDot.IsActive() || ((ripDot.RemainingDuration(sim) < ripDot.TickLength) && !isExecutePhase)) && (simTimeRemain >= endThresh) && ripCcCheck
	biteAtEnd := (curCp >= rotation.MinCombosForBite) && ((simTimeRemain < endThresh) || (ripDot.IsActive() && (simTimeRemain-ripDot.RemainingDuration(sim) < endThresh)))

	// Clip Mangle if it won't change the total number of Mangles we have to
	// cast before the fight ends.
	mangleRefreshNow := !cat.bleedAura.IsActive() && simTimeRemain > time.Second
	mangleRefreshPending := cat.bleedAura.IsActive() && cat.bleedAura.RemainingDuration(sim) < (simTimeRemain-time.Second)
	clipMangle := false

	if mangleRefreshPending {
		numManglesRemaining := int((time.Second + (simTimeRemain - cat.bleedAura.RemainingDuration(sim) - time.Second)).Minutes())
		earliestMangle := (sim.GetRemainingDuration() + sim.CurrentTime) - time.Duration(numManglesRemaining)*time.Minute
		clipMangle = sim.CurrentTime >= earliestMangle
	}

	mangleNow := !ripNow && cat.MangleCat != nil && (mangleRefreshNow || clipMangle)

	biteBeforeRip := (curCp >= rotation.MinCombosForBite) && ripDot.IsActive() && cat.SavageRoarAura.IsActive() && (rotation.UseBite || isExecutePhase) && cat.canBite(sim, isExecutePhase)
	biteNow := (biteBeforeRip || biteAtEnd) && !isClearcast && curEnergy < 67

	// During Berserk, we additionally add an Energy constraint on Bite
	// usage to maximize the total Energy expenditure we can get.
	if biteNow && cat.BerserkAura.IsActive() {
		biteNow = curEnergy <= rotation.BerserkBiteThresh
	}

	// Ignore minimum CP enforcement during Execute phase if Rip is about to fall off
	emergencyBiteNow := isExecutePhase && ripDot.IsActive() && (ripDot.RemainingDuration(sim) < ripDot.TickLength) && (curCp >= 1)
	biteNow = biteNow || emergencyBiteNow

	rakeNow := rotation.UseRake && (!rakeDot.IsActive() || (rakeDot.RemainingDuration(sim) < rakeDot.TickLength)) && (simTimeRemain > rakeDot.Duration) && rakeCcCheck

	// Additionally, don't Rake if the current Shred DPE is higher due to
	// trinket procs etc.
	if rotation.RakeDpeCheck && rakeNow {
		rakeDpe, shredDpe := cat.calcBuilderDpe(sim)
		rakeNow = (rakeDpe > shredDpe)
	}

	// Additionally, don't Rake if there is insufficient time to max out
	// our available glyph of shred extensions before rip falls off
	if rakeNow && ripDot.IsActive() {
		remainingExt := cat.maxRipTicks - ripDot.NumberOfTicks
		remainingRipDur := ripDot.RemainingDuration(sim) + time.Duration(remainingExt) * ripDot.TickLength
		energyForShreds := curEnergy - cat.CurrentRakeCost() - cat.Rip.DefaultCast.Cost + remainingRipDur.Seconds() * regenRate + core.Ternary(cat.tfExpectedBefore(sim, sim.CurrentTime + remainingRipDur), 60.0, 0.0)
		maxShredsPossible := min(energyForShreds/cat.Shred.DefaultCast.Cost, (ripDot.ExpiresAt() - (sim.CurrentTime + time.Second)).Seconds())
		rakeNow = remainingExt == 0 || (maxShredsPossible > float64(remainingExt))
	}

	// Disable Energy pooling for Rake in weaving rotations, since these
	// rotations prioritize weave cpm over Rake uptime.
	poolForRake := (rotation.BearweaveType == proto.FeralDruid_Rotation_None)

	roarNow := curCp >= 1 && (!cat.SavageRoarAura.IsActive() || cat.clipRoar(sim, isExecutePhase))

	// Keep up Sunder debuff if not provided externally
	ffNow := rotation.MaintainFaerieFire && cat.ShouldFaerieFire(sim, cat.CurrentTarget)

	// Pooling calcs
	ripRefreshPending := ripDot.IsActive() && (ripDot.RemainingDuration(sim) < simTimeRemain - endThresh) && (curCp >= core.TernaryInt32(isExecutePhase, 1, rotation.MinCombosForRip))
	rakeRefreshPending := rakeDot.IsActive() && (rakeDot.RemainingDuration(sim) < simTimeRemain - rakeDot.Duration)
	pendingPool := PoolingActions{}
	pendingPool.create(4)

	if ripRefreshPending && (ripDot.RemainingDuration(sim) > ripDot.TickLength) {
		ripRefreshTime := ripDot.ExpiresAt() - ripDot.TickLength
		baseCost := core.Ternary(isExecutePhase, cat.FerociousBite.DefaultCast.Cost, cat.Rip.DefaultCast.Cost)
		refreshCost := core.Ternary(cat.berserkExpectedAt(sim, ripRefreshTime), baseCost*0.5, baseCost)
		pendingPool.addAction(ripRefreshTime, refreshCost)
	}
	if poolForRake && rakeRefreshPending && (rakeDot.RemainingDuration(sim) > rakeDot.TickLength) {
		rakeRefreshTime := rakeDot.ExpiresAt() - rakeDot.TickLength
		rakeCost := core.Ternary(cat.berserkExpectedAt(sim, rakeRefreshTime), cat.Rake.DefaultCast.Cost*0.5, cat.Rake.DefaultCast.Cost)
		pendingPool.addAction(rakeRefreshTime, rakeCost)
	}
	if mangleRefreshPending {
		mangleCost := core.Ternary(cat.berserkExpectedAt(sim, cat.bleedAura.ExpiresAt()), cat.MangleCat.DefaultCast.Cost*0.5, cat.MangleCat.DefaultCast.Cost)
		pendingPool.addAction(cat.bleedAura.ExpiresAt(), mangleCost)
	}
	if cat.SavageRoarAura.IsActive() {
		roarCost := core.Ternary(cat.berserkExpectedAt(sim, cat.SavageRoarAura.ExpiresAt()), cat.SavageRoar.DefaultCast.Cost*0.5, cat.SavageRoar.DefaultCast.Cost)
		pendingPool.addAction(cat.SavageRoarAura.ExpiresAt(), roarCost)
	}

	pendingPool.sort()
	floatingEnergy := pendingPool.calcFloatingEnergy(cat, sim)
	excessE := curEnergy - floatingEnergy
	latencySecs := cat.ReactionTime.Seconds()

	// Allow for bearweaving if the next pending action is >= 4.5s away
	furorCap := min(100.0*float64(cat.Talents.Furor)/3.0, 85)
	weaveEnergy := furorCap - 30 - 20*latencySecs

	// With 3/3 Furor, force 2-GCD bearweaves whenever possible
	if cat.Talents.Furor == 3 {
		weaveEnergy -= 15.0

		// Force a 3-GCD weave when stacking Lacerates for the first time
		if rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && !lacerateDot.IsActive() {
			weaveEnergy -= 15.0
		}
	}

	weaveEnd := time.Duration(float64(sim.CurrentTime) + (4.5+2*latencySecs)*float64(time.Second))
	bearweaveNow := rotation.BearweaveType != proto.FeralDruid_Rotation_None && curEnergy <= weaveEnergy && !isClearcast && (ripRefreshPending || ripDot.ExpiresAt() >= weaveEnd) && !cat.BerserkAura.IsActive()

	if bearweaveNow && rotation.BearweaveType != proto.FeralDruid_Rotation_Lacerate {
		bearweaveNow = !cat.tfExpectedBefore(sim, weaveEnd)
	}

	// Also add an end of fight condition to make sure we can spend down our
	// Energy post-bearweave before the encounter ends. Time to spend is
	// given by weave_end plus 1 second per 42 Energy that we have at
	// weave_end.
	if bearweaveNow {
		energyToDump := curEnergy + ((weaveEnd - sim.CurrentTime).Seconds() * 10)
		bearweaveNow = weaveEnd+time.Duration(math.Floor(energyToDump/42)*float64(time.Second)) < sim.CurrentTime+simTimeRemain
	}

	// If we're maintaining Lacerate, then allow for emergency bearweaves
	// if Lacerate is about to fall off even if the above conditions do not
	// apply.
	lacRemain := core.Ternary(lacerateDot.IsActive(), lacerateDot.RemainingDuration(sim), time.Duration(0))
	emergencyBearweave := rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && lacerateDot.IsActive() && (float64(lacRemain) < (2.5+latencySecs)*float64(time.Second)) && (lacRemain < simTimeRemain) && !cat.BerserkAura.IsActive()

	if bearweaveNow || emergencyBearweave {
		// oom check, if we arent able to shift into bear and back
		// then abandon bearweave
		if cat.CurrentMana() < shiftCost*2.0 {
			bearweaveNow = false
			emergencyBearweave = false
			cat.Metrics.MarkOOM(sim)
		}
	}

	// Main  decision tree starts here
	timeToNextAction := time.Duration(0)

	if !cat.CatFormAura.IsActive() {
		// Shift back into Cat Form if (a) our first bear auto procced
		// Clearcasting, or (b) our first bear auto didn't generate enough
		// Rage to Mangle or Maul, or (c) we don't have enough time or
		// Energy leeway to spend an additional GCD in Dire Bear Form.
		shiftNow := (curEnergy+15.0+(10.0*latencySecs) > furorCap) || (ripRefreshPending && (ripDot.RemainingDuration(sim) < (3.0 * time.Second))) || cat.BerserkAura.IsActive()
		shiftNext := (curEnergy+30.0+(10.0*latencySecs) > furorCap) || (ripRefreshPending && (ripDot.RemainingDuration(sim) < time.Duration(4500*time.Millisecond))) || cat.BerserkAura.IsActive()

		var powerbearNow bool
		if rotation.Powerbear {
			powerbearNow = !shiftNow && curRage < 10
		} else {
			powerbearNow = false
			shiftNow = shiftNow || curRage < 10
		}

		buildLacerate := !lacerateDot.IsActive() || lacerateDot.GetStacks() < 5
		maintainLacerate := !buildLacerate && (lacRemain <= rotation.LacerateTime) && (curRage < 38 || shiftNext) && (lacRemain < simTimeRemain)

		lacerateNow := rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && (buildLacerate || maintainLacerate)
		emergencyLacerate := rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && lacerateDot.IsActive() && (lacRemain < 3*time.Second+2*cat.ReactionTime) && lacRemain < simTimeRemain

		if (rotation.BearweaveType != proto.FeralDruid_Rotation_Lacerate) || !lacerateNow {
			shiftNow = shiftNow || isClearcast
		}

		// Also add an end of fight condition to prevent extending a weave
		// if we don't have enough time to spend the pooled Energy thus far.
		if !shiftNow {
			energyToDump := curEnergy + 30 + 10*latencySecs
			timeToDump := (3 * time.Second) + cat.ReactionTime + time.Duration(math.Floor(energyToDump/42)*float64(time.Second))
			shiftNow = timeToDump >= simTimeRemain
		}

		nextSwing := cat.AutoAttacks.NextAttackAt()

		if emergencyLacerate && cat.Lacerate.CanCast(sim, cat.CurrentTarget) {
			cat.Lacerate.Cast(sim, cat.CurrentTarget)
			return false, 0
		} else if shiftNow {
			// If we are resetting our swing timer using Albino Snake or a
			// duplicate weapon swap, then do an additional check here to
			// see whether we can delay the shift until the next bear swing
			// goes out in order to maximize the gains from the reset.
			projectedDelay := nextSwing + 2*cat.ReactionTime - sim.CurrentTime
			ripConflict := ripRefreshPending && (ripDot.ExpiresAt() < sim.CurrentTime+projectedDelay+(1500*time.Millisecond))
			nextCatSwing := sim.CurrentTime + cat.ReactionTime + time.Duration(float64(cat.AutoAttacks.MainhandSwingSpeed())/float64(2500*time.Millisecond))
			canDelayShift := !ripConflict && cat.Rotation.SnekWeave && (curEnergy+10*projectedDelay.Seconds() <= furorCap) && (nextSwing < nextCatSwing)

			if canDelayShift {
				timeToNextAction = nextSwing - sim.CurrentTime
			} else {
				cat.readyToShift = true
			}
		} else if powerbearNow {
			cat.shiftBearCat(sim, true)
		} else if lacerateNow && cat.Lacerate.CanCast(sim, cat.CurrentTarget) {
			cat.Lacerate.Cast(sim, cat.CurrentTarget)
			return false, 0
		} else if cat.MangleBear.CanCast(sim, cat.CurrentTarget) {
			cat.MangleBear.Cast(sim, cat.CurrentTarget)
			return false, 0
		} else if cat.Lacerate.CanCast(sim, cat.CurrentTarget) {
			cat.Lacerate.Cast(sim, cat.CurrentTarget)
			return false, 0
		} else {
			timeToNextAction = nextSwing - sim.CurrentTime
		}
	} else if emergencyBearweave {
		cat.readyToShift = true
	} else if ffNow {
		cat.FaerieFire.Cast(sim, cat.CurrentTarget)
		return false, 0
	} else if roarNow {
		if cat.SavageRoar.CanCast(sim, cat.CurrentTarget) {
			cat.SavageRoar.Cast(sim, nil)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentSavageRoarCost() - curEnergy) / regenRate)
	} else if ripNow {
		if cat.Rip.CanCast(sim, cat.CurrentTarget) {
			cat.Rip.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentRipCost() - curEnergy) / regenRate)
	} else if biteNow {
		if cat.FerociousBite.CanCast(sim, cat.CurrentTarget) {
			cat.FerociousBite.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentFerociousBiteCost() - curEnergy) / regenRate)
	} else if mangleNow {
		if cat.MangleCat.CanCast(sim, cat.CurrentTarget) {
			cat.MangleCat.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentMangleCatCost() - curEnergy) / regenRate)
	} else if rakeNow {
		if cat.Rake.CanCast(sim, cat.CurrentTarget) {
			cat.Rake.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentRakeCost() - curEnergy) / regenRate)
	} else if bearweaveNow {
		cat.readyToShift = true
	} else if (rotation.MangleSpam && !isClearcast) || cat.PseudoStats.InFrontOfTarget {
		if cat.MangleCat != nil && excessE >= cat.CurrentMangleCatCost() {
			cat.MangleCat.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		timeToNextAction = core.DurationFromSeconds((cat.CurrentMangleCatCost() - excessE) / regenRate)
	} else {
		if excessE >= cat.CurrentShredCost() || isClearcast {
			cat.Shred.Cast(sim, cat.CurrentTarget)
			return false, 0
		}
		// Also Shred if we're about to cap on Energy. Catches some edge
		// cases where floating_energy > 100 due to too many synced timers.
		if curEnergy > cat.MaximumEnergy() - regenRate * latencySecs {
			cat.Shred.Cast(sim, cat.CurrentTarget)
			return false, 0
		}

		timeToNextAction = core.DurationFromSeconds((cat.CurrentShredCost() - excessE) / regenRate)

		// When Lacerateweaving, there are scenarios where Lacerate is
		// synced with other pending actions. When this happens, pooling for
		// the pending action will inevitably lead to capping on Energy,
		// since we will be forced to shift into Dire Bear Form immediately
		// after pooling in order to save the Lacerate. Instead, it is
		// preferable to just Shred and bearweave early.
		nextCastEnd := sim.CurrentTime + timeToNextAction + cat.ReactionTime + time.Second*2
		ignorePooling := cat.BerserkAura.IsActive() || (rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && lacerateDot.IsActive() && (lacerateDot.ExpiresAt().Seconds()-1.5-latencySecs <= nextCastEnd.Seconds()))

		if ignorePooling {
			if curEnergy >= cat.CurrentShredCost() {
				cat.Shred.Cast(sim, cat.CurrentTarget)
				return false, 0
			}
			timeToNextAction = core.DurationFromSeconds((cat.CurrentShredCost() - curEnergy) / regenRate)
		}
	}

	// Model in latency when waiting on Energy for our next action
	nextAction := sim.CurrentTime + timeToNextAction
	paValid, rt := pendingPool.nextRefreshTime()
	if paValid {
		nextAction = min(nextAction, rt)
	}

	// If Lacerateweaving, then also schedule an action just before Lacerate
	// expires to ensure we can save it in time.
	lacRefreshTime := lacerateDot.ExpiresAt() - (1500 * time.Millisecond) - (3 * cat.ReactionTime)
	if rotation.BearweaveType == proto.FeralDruid_Rotation_Lacerate && lacerateDot.IsActive() && lacerateDot.RemainingDuration(sim) < simTimeRemain && (sim.CurrentTime < lacRefreshTime) {
		nextAction = min(nextAction, lacRefreshTime)
	}

	return true, nextAction
}

type FeralDruidRotation struct {
	RotationType proto.FeralDruid_Rotation_AplType

	BearweaveType      proto.FeralDruid_Rotation_BearweaveType
	MaintainFaerieFire bool
	MinCombosForRip    int32
	UseRake            bool
	UseBite            bool
	BiteTime           time.Duration
	BiteDuringExecute  bool
	MinCombosForBite   int32
	MangleSpam         bool
	BerserkBiteThresh  float64
	Powerbear          bool
	MinRoarOffset      time.Duration
	RipLeeway          time.Duration
	LacerateTime       time.Duration
	SnekWeave          bool
	RakeDpeCheck       bool

	AoeMangleBuilder bool
}

func (cat *FeralDruid) setupRotation(rotation *proto.FeralDruid_Rotation) {
	// Force reset params that aren't customizable, or removed from ui
	rotation.BerserkBiteThresh = 25
	rotation.BearWeaveType = proto.FeralDruid_Rotation_None

	equipedIdol := cat.Ranged().ID

	cat.Rotation = FeralDruidRotation{
		RotationType:       rotation.RotationType,
		BearweaveType:      rotation.BearWeaveType,
		MaintainFaerieFire: rotation.MaintainFaerieFire,
		MinCombosForRip:    5,
		UseRake:            rotation.UseRake,
		UseBite:            rotation.UseBite,
		BiteTime:           time.Duration(float64(rotation.BiteTime) * float64(time.Second)),
		BiteDuringExecute:  core.Ternary(cat.Talents.BloodInTheWater > 0, rotation.BiteDuringExecute, false),
		MinCombosForBite:   5,
		MangleSpam:         rotation.MangleSpam,
		BerserkBiteThresh:  float64(rotation.BerserkBiteThresh),
		Powerbear:          rotation.Powerbear,
		MinRoarOffset:      time.Duration(float64(rotation.MinRoarOffset) * float64(time.Second)),
		RipLeeway:          time.Duration(float64(rotation.RipLeeway) * float64(time.Second)),
		LacerateTime:       8.0 * time.Second,
		SnekWeave:          core.Ternary(rotation.BearWeaveType == proto.FeralDruid_Rotation_None, false, rotation.SnekWeave),
		// Use mangle if idol of corruptor or mutilation equipped
		AoeMangleBuilder: equipedIdol == 45509 || equipedIdol == 47668,
		RakeDpeCheck:     equipedIdol != 50456,
	}

	// Use automatic values unless specified
	if rotation.ManualParams {
		return
	}

	cat.Rotation.UseRake = true
	cat.Rotation.UseBite = true
	cat.Rotation.BiteDuringExecute = (cat.Talents.BloodInTheWater == 2)

	cat.Rotation.RipLeeway = 4 * time.Second
	cat.Rotation.MinRoarOffset = 12 * time.Second
	cat.Rotation.BiteTime = 10 * time.Second
}
