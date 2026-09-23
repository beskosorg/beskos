import { describe, it, expect } from "vitest";

// The matcher that turns the AI's suggested assignee into a real user id. Copied from
// ExtractTasksModal so it can be exercised without React; the logic is what matters and it is the
// piece that was silently failing -- "Dener" never matched a user displayed as "D Fernandes", and
// the task was created with nobody on it while the screen looked fine.
type Colleague = { id: string; name: string; email: string };

function resolveAssignee(raw: string, list: Colleague[]): string {
  const n = raw.trim().toLowerCase().replace(/^@/, "");
  if (!n) return "";
  const exact = list.find((c) => c.email.toLowerCase() === n || c.name.toLowerCase() === n);
  if (exact) return exact.id;
  const token = list.filter((c) =>
    c.name.toLowerCase().split(/\s+/).includes(n) || c.email.toLowerCase().split("@")[0] === n);
  if (token.length === 1) return token[0].id;
  const starts = list.filter((c) => c.name.toLowerCase().startsWith(n) || c.email.toLowerCase().startsWith(n));
  return starts.length === 1 ? starts[0].id : "";
}

const people: Colleague[] = [
  { id: "1", name: "D Fernandes", email: "dfernandes@beskos.com" },
  { id: "2", name: "Ana Silva", email: "ana@beskos.com" },
  { id: "3", name: "Ana Costa", email: "anac@beskos.com" },
];

describe("resolveAssignee", () => {
  it("matches the e-mail the server now asks the model for", () => {
    expect(resolveAssignee("dfernandes@beskos.com", people)).toBe("1");
  });

  // The case from the report: someone writes a first name, the account shows a different one.
  it("matches the local part of the address", () => {
    expect(resolveAssignee("dfernandes", people)).toBe("1");
    expect(resolveAssignee("@dfernandes", people)).toBe("1");
  });

  it("matches one word of the display name", () => {
    expect(resolveAssignee("Fernandes", people)).toBe("1");
    expect(resolveAssignee("Silva", people)).toBe("2");
  });

  // Two people called Ana: picking one would be a decision nobody reviews, because a filled-in
  // owner is not something anyone goes back to check.
  it("refuses an ambiguous first name instead of guessing", () => {
    expect(resolveAssignee("Ana", people)).toBe("");
  });

  it("resolves a prefix only when it is unambiguous", () => {
    expect(resolveAssignee("Fern", people)).toBe("");   // matches no name start
    expect(resolveAssignee("D Fern", people)).toBe("1");
  });

  it("gives nothing for an unknown name and for empty input", () => {
    expect(resolveAssignee("Nobody", people)).toBe("");
    expect(resolveAssignee("   ", people)).toBe("");
  });
});
