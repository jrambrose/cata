import * as PresetUtils from '../../core/preset_utils';
import { ConsumesSpec, Glyphs, Profession, PseudoStat, Race, Spec, Stat } from '../../core/proto/common';
import { DeathKnightMajorGlyph, DeathKnightMinorGlyph, UnholyDeathKnight_Options } from '../../core/proto/death_knight';
import { SavedTalents } from '../../core/proto/ui';
import { Stats } from '../../core/proto_utils/stats';
import DefaultApl from '../../death_knight/unholy/apls/default.apl.json';
import P1Build from '../../death_knight/unholy/builds/p1.build.json';
import PrebisBuild from '../../death_knight/unholy/builds/prebis.build.json';
import P1Gear from '../../death_knight/unholy/gear_sets/p1.gear.json';
import PrebisGear from '../../death_knight/unholy/gear_sets/prebis.gear.json';

// Preset options for this spec.
// Eventually we will import these values for the raid sim too, so its good to
// keep them in a separate file.
export const PREBIS_GEAR_PRESET = PresetUtils.makePresetGear('Prebis', PrebisGear);
export const P1_BIS_GEAR_PRESET = PresetUtils.makePresetGear('P1', P1Gear);

export const DEFAULT_ROTATION_PRESET = PresetUtils.makePresetAPLRotation('Default', DefaultApl);

// Preset options for EP weights
export const P1_UNHOLY_EP_PRESET = PresetUtils.makePresetEpWeights(
	'P1',
	Stats.fromMap(
		{
			[Stat.StatStrength]: 1.0,
			[Stat.StatHitRating]: 0.73,
			[Stat.StatExpertiseRating]: 0.73,
			[Stat.StatCritRating]: 0.46,
			[Stat.StatHasteRating]: 0.47,
			[Stat.StatMasteryRating]: 0.41,
			[Stat.StatAttackPower]: 0.3,
		},
		{
			[PseudoStat.PseudoStatMainHandDps]: 0.8,
		},
	),
);

// Default talents. Uses the wowhead calculator format, make the talents on
// https://wotlk.wowhead.com/talent-calc and copy the numbers in the url.

export const DefaultTalents = {
	name: 'Default',
	data: SavedTalents.create({
		talentsString: '221111',
		glyphs: Glyphs.create({
			major1: DeathKnightMajorGlyph.GlyphOfAntiMagicShell,
			major2: DeathKnightMajorGlyph.GlyphOfPestilence,
			major3: DeathKnightMajorGlyph.GlyphOfLoudHorn,
			minor1: DeathKnightMinorGlyph.GlyphOfArmyOfTheDead,
			minor2: DeathKnightMinorGlyph.GlyphOfTranquilGrip,
			minor3: DeathKnightMinorGlyph.GlyphOfDeathsEmbrace,
		}),
	}),
};

export const PREBIS_PRESET = PresetUtils.makePresetBuildFromJSON('Prebis', Spec.SpecUnholyDeathKnight, PrebisBuild, {
	epWeights: P1_UNHOLY_EP_PRESET,
});
export const P1_PRESET = PresetUtils.makePresetBuildFromJSON('P1', Spec.SpecUnholyDeathKnight, P1Build, {
	epWeights: P1_UNHOLY_EP_PRESET,
});

export const DefaultOptions = UnholyDeathKnight_Options.create({
	classOptions: {},
});

export const OtherDefaults = {
	profession1: Profession.Engineering,
	profession2: Profession.Blacksmithing,
	distanceFromTarget: 5,
	race: Race.RaceOrc,
};

export const DefaultConsumables = ConsumesSpec.create({
	flaskId: 76088, // Flask of Winter's Bite
	foodId: 74646, // Black Pepper Ribs and Shrimp
	potId: 76095, // Potion of Mogu Power
	prepotId: 76095, // Potion of Mogu Power
});
