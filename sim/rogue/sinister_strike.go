package rogue

import (
	"time"

	"github.com/wowsims/cata/sim/core"
	"github.com/wowsims/cata/sim/core/proto"
)

func (rogue *Rogue) registerSinisterStrikeSpell() {
	hasGlyphOfSinisterStrike := rogue.HasPrimeGlyph(proto.RoguePrimeGlyph_GlyphOfSinisterStrike)
	baseDamage := RogueBaseDamageScalar * 0.1780000031
	t11Bonus := core.TernaryFloat64(rogue.HasSetBonus(Tier11, 2), 5*core.CritRatingPerCritChance, 0)

	rogue.SinisterStrike = rogue.RegisterSpell(core.SpellConfig{
		ActionID:    core.ActionID{SpellID: 1752},
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskMeleeMHSpecial,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagIncludeTargetBonusDamage | SpellFlagBuilder | SpellFlagColdBlooded | core.SpellFlagAPL,

		EnergyCost: core.EnergyCostOptions{
			Cost:   rogue.GetGeneratorCostModifier(45 - 2*float64(rogue.Talents.ImprovedSinisterStrike)),
			Refund: 0.8,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: time.Second,
			},
			IgnoreHaste: true,
		},

		BonusCritRating:  t11Bonus,
		DamageMultiplier: 1.04, // 84 * .73500001431 + 42
		DamageMultiplierAdditive: 1 +
			[]float64{0.0, .07, .14, .20}[rogue.Talents.Aggression] +
			0.01*float64(rogue.Talents.ImprovedSinisterStrike),
		CritMultiplier:   rogue.MeleeCritMultiplier(true),
		ThreatMultiplier: 1,

		BonusCoefficient: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			rogue.BreakStealth(sim)
			baseDamage := baseDamage +
				spell.Unit.MHNormalizedWeaponDamage(sim, spell.MeleeAttackPower())

			result := spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeWeaponSpecialHitAndCrit)

			if result.Landed() {
				points := int32(1)
				if hasGlyphOfSinisterStrike {
					if sim.RandomFloat("Glyph of Sinister Strike") < 0.2 {
						points += 1
					}
				}
				rogue.AddComboPoints(sim, points, spell.ComboPointMetrics())
			} else {
				spell.IssueRefund(sim)
			}
		},
	})
}
