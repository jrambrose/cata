package shaman

import (
	"time"

	"github.com/wowsims/cata/sim/core"
)

const (
	// This could be value or bitflag if we ended up needing multiple flags at the same time.
	//1 to 5 are used by MaelstromWeapon Stacks
	CastTagLightningOverload int32 = 6
)

// Shared precomputation logic for LB and CL.
func (shaman *Shaman) newElectricSpellConfig(actionID core.ActionID, baseCost float64, baseCastTime time.Duration, isLightningOverload bool) core.SpellConfig {
	mask := core.ProcMaskSpellDamage
	if isLightningOverload {
		mask = core.ProcMaskProc
	}
	flags := SpellFlagElectric | SpellFlagFocusable
	if !isLightningOverload {
		flags |= core.SpellFlagAPL
	}
	spell := core.SpellConfig{
		ActionID:     actionID,
		SpellSchool:  core.SpellSchoolNature,
		ProcMask:     mask,
		Flags:        flags,
		MetricSplits: 6,

		ManaCost: core.ManaCostOptions{
			BaseCost:   core.TernaryFloat64(isLightningOverload, 0, baseCost),
			Multiplier: 1 - 0.05*float64(shaman.Talents.Convection),
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				CastTime: baseCastTime - time.Millisecond*100*time.Duration(shaman.Talents.LightningMastery),
				GCD:      core.GCDDefault,
			},
			ModifyCast: func(sim *core.Simulation, spell *core.Spell, cast *core.Cast) {
				spell.SetMetricsSplit(shaman.MaelstromWeaponAura.GetStacks())
			},
		},

		BonusHitRating: float64(shaman.Talents.ElementalPrecision) * core.SpellHitRatingPerHitChance,
		BonusCritRating: 0 +
			float64(shaman.Talents.TidalMastery)*core.CritRatingPerCritChance +
			core.TernaryFloat64(shaman.Talents.CallOfThunder, 5*core.CritRatingPerCritChance, 0),
		DamageMultiplier: 1 + 0.02*float64(shaman.Talents.Concussion),
		CritMultiplier:   shaman.ElementalCritMultiplier(0),
		ThreatMultiplier: shaman.spellThreatMultiplier(),
	}

	if isLightningOverload {
		spell.ActionID.Tag = CastTagLightningOverload
		spell.Cast.DefaultCast.CastTime = 0
		spell.Cast.DefaultCast.GCD = 0
		spell.Cast.DefaultCast.Cost = 0
		spell.Cast.ModifyCast = nil
		spell.MetricSplits = 0
		spell.DamageMultiplier *= 0.5
		spell.ThreatMultiplier = 0
	}

	return spell
}
