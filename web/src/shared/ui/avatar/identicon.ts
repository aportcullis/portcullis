export type Identicon = {
  color: string;
  cells: readonly { x: number; y: number }[];
};

/** Creates a stable geometric avatar from an opaque identity. */
export function createIdenticon(seed: string): Identicon {
  let hash = 2166136261;
  for (const character of seed) {
    hash = Math.imul(hash ^ character.charCodeAt(0), 16777619) >>> 0;
  }
  let state = hash || 1;
  const cells: { x: number; y: number }[] = [];
  for (let y = 0; y < 5; y++) {
    for (let x = 0; x < 3; x++) {
      state ^= state << 13;
      state ^= state >>> 17;
      state ^= state << 5;
      if ((state & 1) === 0 && !(x === 2 && y === 2)) continue;
      cells.push({ x, y });
      if (x !== 2) cells.push({ x: 4 - x, y });
    }
  }
  return { color: `hsl(${hash % 360} 60% 40%)`, cells };
}
