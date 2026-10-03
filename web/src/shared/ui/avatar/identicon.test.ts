import { describe, expect, it } from "vitest";

import { createIdenticon } from "@/shared/ui/avatar/identicon";

describe("default profile image", () => {
  it("keeps the same appearance across reloads for the same identity", () => {
    expect(createIdenticon("user-one")).toEqual(createIdenticon("user-one"));
  });

  it("distinguishes different users without an external image service", () => {
    const avatars = new Set(Array.from({ length: 32 }, (_, user) => {
      const avatar = createIdenticon(`user-${user}`);
      return `${avatar.color}:${avatar.cells.map(({ x, y }) => `${x},${y}`).join(";")}`;
    }));
    expect(avatars.size).toBe(32);
  });

  it("produces a visible symmetric five-by-five pattern with bounded cells", () => {
    for (const seed of ["user-one", "user-two", "", "사용자"]) {
      const avatar = createIdenticon(seed);
      expect(avatar.cells.length).toBeGreaterThan(0);
      expect(avatar.cells.length).toBeLessThanOrEqual(25);
      expect(new Set(avatar.cells.map(({ x, y }) => `${x},${y}`)).size).toBe(avatar.cells.length);
      for (const cell of avatar.cells) {
        expect(Number.isInteger(cell.x) && cell.x >= 0 && cell.x < 5).toBe(true);
        expect(Number.isInteger(cell.y) && cell.y >= 0 && cell.y < 5).toBe(true);
        expect(avatar.cells).toContainEqual({ x: 4 - cell.x, y: cell.y });
      }
    }
  });
});
