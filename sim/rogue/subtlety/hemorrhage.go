package subtlety

import (
	"time"

	"github.com/wowsims/cata/sim/core"
	"github.com/wowsims/cata/sim/core/proto"
	"github.com/wowsims/cata/sim/rogue"
)

func (subRogue *SubtletyRogue) registerHemorrhageSpell() {
	if !subRogue.Talents.Hemorrhage {
		return
	}

	hemoActionID := core.ActionID{SpellID: 16511}
	hemoDotActionID := core.ActionID{SpellID: 89775}
	hasGlyph := subRogue.HasPrimeGlyph(proto.RoguePrimeGlyph_GlyphOfHemorrhage)
	hemoAuras := subRogue.NewEnemyAuraArray(core.HemorrhageAura)
	var lastHemoDamage float64

	// Hemorrhage DoT has a chance to proc MH weapon effects/poisons, so must be defined as its own spell
	hemoDot := subRogue.RegisterSpell(core.SpellConfig{
		ActionID:    hemoDotActionID,
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskMeleeMHSpecial,
		Flags:       core.SpellFlagIgnoreAttackerModifiers, // From initial testing, Hemo DoT only benefits from debuffs on target, such as 30% bleed damage

		ThreatMultiplier: 1,
		CritMultiplier:   1,
		DamageMultiplier: 1,

		Dot: core.DotConfig{
			Aura: core.Aura{
				Label:    "Hemorrhage DoT",
				Tag:      rogue.RogueBleedTag,
				ActionID: core.ActionID{SpellID: 89775},
				Duration: time.Second * 24,
			},
			NumberOfTicks: 8,
			TickLength:    time.Second * 3,

			OnSnapshot: func(sim *core.Simulation, target *core.Unit, dot *core.Dot, _ bool) {
				attackTable := dot.Spell.Unit.AttackTables[target.UnitIndex]
				dot.SnapshotCritChance = dot.Spell.PhysicalCritChance(attackTable)
				dot.SnapshotAttackerMultiplier = 1
			},
			OnTick: func(sim *core.Simulation, target *core.Unit, dot *core.Dot) {
				dot.CalcAndDealPeriodicSnapshotDamage(sim, target, dot.OutcomeSnapshotCrit)
			},
		},

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			dot := spell.Dot(target)
			dot.SnapshotBaseDamage = lastHemoDamage * .05
			dot.Apply(sim)
		},
	})

	subRogue.Rogue.Hemorrhage = subRogue.RegisterSpell(core.SpellConfig{
		ActionID:    hemoActionID,
		SpellSchool: core.SpellSchoolPhysical,
		ProcMask:    core.ProcMaskMeleeMHSpecial,
		Flags:       core.SpellFlagMeleeMetrics | core.SpellFlagIncludeTargetBonusDamage | rogue.SpellFlagBuilder | core.SpellFlagAPL,

		EnergyCost: core.EnergyCostOptions{
			Cost:   subRogue.GetGeneratorCostModifier(35 - 2*float64(subRogue.Talents.SlaughterFromTheShadows)),
			Refund: 0.8,
		},
		Cast: core.CastConfig{
			DefaultCast: core.Cast{
				GCD: time.Second,
			},
			IgnoreHaste: true,
		},

		DamageMultiplier: core.TernaryFloat64(subRogue.HasDagger(core.MainHand), 3.25, 2.24),
		CritMultiplier:   subRogue.MeleeCritMultiplier(true),
		ThreatMultiplier: 1,

		BonusCoefficient: 1,

		ApplyEffects: func(sim *core.Simulation, target *core.Unit, spell *core.Spell) {
			subRogue.BreakStealth(sim)
			baseDamage := 0 +
				spell.Unit.MHNormalizedWeaponDamage(sim, spell.MeleeAttackPower())

			result := spell.CalcAndDealDamage(sim, target, baseDamage, spell.OutcomeMeleeWeaponSpecialHitAndCrit)

			if result.Landed() {
				subRogue.AddComboPoints(sim, 1, spell.ComboPointMetrics())
				hemoAuras.Get(target).Activate(sim)
				if hasGlyph {
					lastHemoDamage = result.Damage
					hemoDot.Cast(sim, target)
				}
			} else {
				spell.IssueRefund(sim)
			}
		},

		RelatedAuras: []core.AuraArray{hemoAuras},
	})
}
