/**
 * Scene colour tokens ("Dusk shift", docs/ux-redesign.md section 2). The scene
 * owns its own constants so it renders correctly whatever the page CSS defines;
 * values are 0xRRGGBB numbers for Pixi.
 */
export interface Palette {
  ink: number;
  surface: number;
  raised: number;
  hairline: number;
  edge: number;
  ivory: number;
  ivoryDim: number;
  amber: number;
  amberHot: number;
  clay: number;
  sage: number;
  floorA: number;
  floorB: number;
  wall: number;
  wallDark: number;
  rack: number;
  rackDark: number;
  slab: number;
  shadow: number;
  glass: number;
}

export const PALETTE: Palette = {
  ink: 0x17161a,
  surface: 0x201f24,
  raised: 0x2a292f,
  hairline: 0x4a4750,
  edge: 0x7a7680,
  ivory: 0xf3ede0,
  ivoryDim: 0xb9b2a3,
  amber: 0xe8a33d,
  amberHot: 0xf5b954,
  clay: 0xee7b63,
  sage: 0x9cc48a,
  floorA: 0x2b2a30,
  floorB: 0x323138,
  wall: 0x3b3a42,
  wallDark: 0x302f36,
  rack: 0x5a5662,
  rackDark: 0x45424d,
  slab: 0x1f1e23,
  shadow: 0x0e0d10,
  glass: 0x4b4350,
};

/**
 * Product box colours (top, left face, right face) and a glyph index, one per
 * product, cycling. Distinct hues avoid clay (crisis) and sage (gain).
 */
export interface BoxColors {
  top: number;
  left: number;
  right: number;
  edge: number;
}
export const BOX_COLORS: readonly BoxColors[] = [
  { top: 0xddba85, left: 0xc9a26b, right: 0xa98550, edge: 0x7d6238 }, // cardboard
  { top: 0x9db8d0, left: 0x7c9bb8, right: 0x5f7e9b, edge: 0x44607a }, // dusty blue
  { top: 0xe6cc6e, left: 0xd0b24a, right: 0xa88f33, edge: 0x7a6a22 }, // mustard
  { top: 0xc3aed2, left: 0xa58fb8, right: 0x86709a, edge: 0x5f4d73 }, // lilac
  { top: 0x86c7be, left: 0x5fafa6, right: 0x468d85, edge: 0x2f655f }, // teal
  { top: 0xd2aaaa, left: 0xb98a8a, right: 0x996c6c, edge: 0x714848 }, // rose grey
];
export const GLYPH_COUNT = 6;

export function readPalette(): Palette {
  return PALETTE;
}
