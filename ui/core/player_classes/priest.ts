import { EligibleWeaponType, IconSize, PlayerClass } from '../player_class';
import { PlayerSpec } from '../player_spec';
import { PlayerSpecs } from '../player_specs';
import { ArmorType, Class, Race, RangedWeaponType, WeaponType } from '../proto/common';
import { PriestSpecs } from '../proto_utils/utils';

export class Priest extends PlayerClass<Class.ClassPriest> {
	static classID = Class.ClassPriest as Class.ClassPriest;
	static friendlyName = 'Priest';
	static hexColor = '#fff';
	static specs: Record<string, PlayerSpec<PriestSpecs>> = {
		[PlayerSpecs.DisciplinePriest.friendlyName]: PlayerSpecs.DisciplinePriest,
		[PlayerSpecs.HolyPriest.friendlyName]: PlayerSpecs.HolyPriest,
		[PlayerSpecs.ShadowPriest.friendlyName]: PlayerSpecs.ShadowPriest,
	};
	static races: Race[] = [
		// [A]
		Race.RaceHuman,
		Race.RaceDwarf,
		Race.RaceNightElf,
		Race.RaceGnome,
		Race.RaceDraenei,
		Race.RaceWorgen,
		// [H]
		Race.RaceUndead,
		Race.RaceTauren,
		Race.RaceTroll,
		Race.RaceBloodElf,
		Race.RaceGoblin,
	];
	static armorTypes: ArmorType[] = [ArmorType.ArmorTypeCloth];
	static weaponTypes: EligibleWeaponType[] = [
		{ weaponType: WeaponType.WeaponTypeDagger },
		{ weaponType: WeaponType.WeaponTypeMace },
		{ weaponType: WeaponType.WeaponTypeOffHand },
		{ weaponType: WeaponType.WeaponTypeStaff, canUseTwoHand: true },
	];
	static rangedWeaponTypes: RangedWeaponType[] = [RangedWeaponType.RangedWeaponTypeWand];

	readonly classID = Priest.classID;
	readonly friendlyName = Priest.name;
	readonly hexColor = Priest.hexColor;
	readonly specs = Priest.specs;
	readonly races = Priest.races;
	readonly armorTypes = Priest.armorTypes;
	readonly weaponTypes = Priest.weaponTypes;
	readonly rangedWeaponTypes = Priest.rangedWeaponTypes;

	static getIcon = (size: IconSize): string => {
		return `https://wow.zamimg.com/images/wow/icons/${size}/class_priest.jpg`;
	};

	getIcon = (size: IconSize): string => {
		return Priest.getIcon(size);
	};
}
