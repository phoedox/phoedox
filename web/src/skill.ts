/** Skill names follow the SKILL.md rules: lowercase letters, digits and
 * hyphens, at most 64, and not "claude" or "anthropic". */
export const SKILL_NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function skillName(title: string): string {
  const map: Record<string, string> = { ä: "ae", ö: "oe", ü: "ue", ß: "ss" };
  const s = title.toLowerCase().replace(/[äöüß]/g, (c) => map[c]).normalize("NFKD").replace(/[̀-ͯ]/g, "")
    .replace(/claude|anthropic/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return s.slice(0, 64).replace(/-+$/, "") || "skill";
}

export const validSkillName = (n: string) => n.length <= 64 && SKILL_NAME.test(n) && !/claude|anthropic/.test(n);
