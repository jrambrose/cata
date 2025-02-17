package death_knight

import (
	"time"

	"github.com/wowsims/cata/sim/core"
	"github.com/wowsims/cata/sim/core/proto"
)

var FesteringStrikeActionID = core.ActionID{SpellID: 85948}

func (dk *DeathKnight) registerFesteringStrikeSpell() {
	ohSpell := dk.GetOrRegisterSpell(core.SpellConfig{
		ActionID:       FesteringStrikeActionID.WithTag(2),
		SpellSchool:    core.SpellSchoolPhysical,
		ProcMask:       core.ProcMaskMeleeOHSpecial,
		Flags:          core.SpellFlagMeleeMetrics,
		ClassSpellMask: DeathKnightSpellFesteringStrike,

		DamageMultiplier: 1.5,
		CritMultiplier:   dk.DefaultMeleeCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			baseDamage := dk.ClassBaseScaling*0.24899999797 +
				spell.Unit.OHNormalizedWeaponDamage(sim, spell.MeleeAttackPower())

			spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeSpecialCritOnly)
		},
	})

	extendHandler := func(aura *core.Aura) {
		aura.UpdateExpires(aura.ExpiresAt() + time.Second*6)
	}

	hasReaping := dk.Inputs.Spec == proto.Spec_SpecUnholyDeathKnight

	dk.GetOrRegisterSpell(core.SpellConfig{
		ActionID:       FesteringStrikeActionID.WithTag(1),
		SpellSchool:    core.SpellSchoolPhysical,
		ProcMask:       core.ProcMaskMeleeMHSpecial,
		Flags:          core.SpellFlagMeleeMetrics | core.SpellFlagAPL,
		ClassSpellMask: DeathKnightSpellFesteringStrike,

		RuneCost: core.RuneCostOptions{
			BloodRuneCost:  1,
			FrostRuneCost:  1,
			RunicPowerGain: 20,
			Refundable:     true,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: core.GCDDefault,
			},
			IgnoreHaste: true,
		},

		DamageMultiplier: 1.5,
		CritMultiplier:   dk.DefaultMeleeCritMultiplier(),
		ThreatMultiplier: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			baseDamage := dk.ClassBaseScaling*0.49799999595 +
				spell.Unit.MHNormalizedWeaponDamage(sim, spell.MeleeAttackPower())

			result := spell.CalcDamage(sim, target, baseDamage, spell.OutcomeMeleeWeaponSpecialHitAndCrit)

			if hasReaping {
				spell.SpendRefundableCostAndConvertBloodOrFrostRune(sim, result, 1)
			} else {
				spell.SpendRefundableCost(sim, result)
			}
			dk.ThreatOfThassarianProc(sim, result, ohSpell)

			if result.Landed() {
				if dk.FrostFeverSpell.Dot(target).IsActive() {
					extendHandler(dk.FrostFeverSpell.Dot(target).Aura)
				}
				if dk.BloodPlagueSpell.Dot(target).IsActive() {
					extendHandler(dk.BloodPlagueSpell.Dot(target).Aura)
				}
				if dk.Talents.EbonPlaguebringer > 0 && dk.EbonPlagueAura.Get(target).IsActive() {
					extendHandler(dk.EbonPlagueAura.Get(target))
				}
			}

			spell.DealDamage(sim, result)
		},
	})
}

// func (dk *DeathKnight) registerDrwPlagueStrikeSpell() {
// 	dk.RuneWeapon.PlagueStrike = dk.RuneWeapon.RegisterSpell(core.SpellConfig{
// 		ActionID:    PlagueStrikeActionID.WithTag(1),
// 		SpellSchool: core.SpellSchoolPhysical,
// 		ProcMask:    core.ProcMaskMeleeMHSpecial,
// 		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagIncludeTargetBonusDamage,

// 		BonusCritRating: (dk.annihilationCritBonus() + dk.scourgebornePlateCritBonus() + dk.viciousStrikesCritChanceBonus()) * core.CritRatingPerCritChance,
// 		DamageMultiplier: 0.5 *
// 			(1.0 + 0.1*float64(dk.Talents.Outbreak)),
// 		CritMultiplier:   dk.bonusCritMultiplier(dk.Talents.ViciousStrikes),
// 		ThreatMultiplier: 1,

// 		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
// 			baseDamage := 378 + dk.DrwWeaponDamage(sim, spell)

// 			result := spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeWeaponSpecialHitAndCrit)

// 			if result.Landed() {
// 				dk.RuneWeapon.BloodPlagueSpell.Cast(sim, target)
// 			}
// 		},
// 	})
// }
