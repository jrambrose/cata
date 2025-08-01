package druid

import (
	"time"

	"github.com/wowsims/mop/sim/core"
)

func (druid *Druid) registerFerociousBiteSpell() {
	// Raw parameters from spell database
	const coefficient = 0.45699998736
	const variance = 0.74000000954
	const resourceCoefficient = 0.69599997997
	const scalingPerComboPoint = 0.196

	// Scaled parameters for spell code
	avgBaseDamage := coefficient * druid.ClassSpellScaling
	damageSpread := variance * avgBaseDamage
	minBaseDamage := avgBaseDamage - damageSpread/2
	dmgPerComboPoint := resourceCoefficient * druid.ClassSpellScaling

	druid.FerociousBite = druid.RegisterSpell(Cat, core.SpellConfig{
		ActionID:       core.ActionID{SpellID: 22568},
		SpellSchool:    core.SpellSchoolPhysical,
		ProcMask:       core.ProcMaskMeleeMHSpecial,
		Flags:          core.SpellFlagMeleeMetrics | core.SpellFlagAPL,
		ClassSpellMask: DruidSpellFerociousBite,

		EnergyCost: core.EnergyCostOptions{
			Cost:   25,
			Refund: 0.8,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: time.Second,
			},
			IgnoreHaste: true,
		},
		ExtraCastCondition: func(sim *core.Simulation, target *core.Unit) bool {
			return druid.ComboPoints() > 0
		},

		DamageMultiplier: 1,
		CritMultiplier:   druid.DefaultCritMultiplier(),
		ThreatMultiplier: 1,
		MaxRange:         core.MaxMeleeRange,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			comboPoints := float64(druid.ComboPoints())
			attackPower := spell.MeleeAttackPower()
			excessEnergy := min(druid.CurrentEnergy(), 25)

			baseDamage := minBaseDamage +
				sim.RandomFloat("Ferocious Bite")*damageSpread +
				dmgPerComboPoint*comboPoints +
				attackPower*scalingPerComboPoint*comboPoints
			baseDamage *= 1.0 + excessEnergy/25

			hasBleed := druid.AssumeBleedActive || (druid.BleedsActive[target] > 0)

			if hasBleed {
				spell.BonusCritPercent += RendAndTearBonusCritPercent
			}

			result := spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeSpecialHitAndCrit)

			if result.Landed() {
				druid.SpendEnergy(sim, excessEnergy, spell.EnergyMetrics())
				druid.SpendComboPoints(sim, spell.ComboPointMetrics())

				// Blood in the Water
				ripDot := druid.Rip.Dot(target)

				if sim.IsExecutePhase25() && ripDot.IsActive() {
					ripDot.BaseTickCount = RipBaseNumTicks
					ripDot.ApplyRollover(sim)
				}
			} else {
				spell.IssueRefund(sim)
			}

			if hasBleed {
				spell.BonusCritPercent -= RendAndTearBonusCritPercent
			}
		},

		ExpectedInitialDamage: func(sim *core.Simulation, target *core.Unit, spell *core.Spell, _ bool) *core.SpellResult {
			// Assume no excess Energy spend, let the user handle that
			comboPoints := float64(druid.ComboPoints())
			attackPower := spell.MeleeAttackPower()
			baseDamage := avgBaseDamage + comboPoints*(dmgPerComboPoint+attackPower*scalingPerComboPoint)
			result := spell.CalcDamage(sim, target, baseDamage, spell.OutcomeExpectedMagicAlwaysHit)
			attackTable := spell.Unit.AttackTables[target.UnitIndex]
			critChance := spell.PhysicalCritChance(attackTable)

			if druid.AssumeBleedActive || (druid.BleedsActive[target] > 0) {
				critChance += RendAndTearBonusCritPercent / 100
			}

			critMod := critChance * (spell.CritMultiplier - 1)
			result.Damage *= 1 + critMod
			return result
		},
	})
}

func (druid *Druid) CurrentFerociousBiteCost() float64 {
	return druid.FerociousBite.Cost.GetCurrentCost()
}
