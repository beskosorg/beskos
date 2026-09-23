// Client-side password strength.
//
// The mail server (Stalwart) measures with zxcvbn and REJECTS below the configured minimum — that
// was what returned "Bad gateway" when creating mailboxes. This function exists so the person
// knows BEFORE submitting, and it is intentionally stricter than the server: better to reject
// here, with an explanation, than to let it through and get its rejection instead.
//
// We don't bring zxcvbn (~800 KB with dictionaries) into a text field: we replicate the signals
// that actually matter — length, variety, repetitions, sequences, common words, and the address of
// whoever is choosing the password.

export type Score = 0 | 1 | 2 | 3 | 4;

export interface Strength {
  score: Score;
  /** Acceptable for submission (the server rejects below this). */
  ok: boolean;
  /** i18n key for what needs improving ("" when it's fine). */
  hint: string;
}

/** Accepted minimum: below 12 characters, it isn't even worth measuring the rest. */
export const MIN_LENGTH = 12;

// The world's most used passwords + the obvious ones in Portuguese. Deliberately short list: it's
// meant to catch carelessness ("password123"), not to act as a dictionary.
const COMMON = [
  "password", "passwd", "senha", "palavra-passe", "qwerty", "azerty", "letmein", "welcome",
  "admin", "administrator", "iloveyou", "sunshine", "princess", "dragon", "monkey", "football",
  "abcabc", "123456", "12345678", "111111", "000000", "beskos", "correio", "email",
];

const SEQUENCES = ["abcdefghijklmnopqrstuvwxyz", "0123456789", "qwertyuiop", "asdfghjkl", "zxcvbnm"];

// Block repetition: "abcabcabc", "ha-ha-ha". This is the case Stalwart cited in its rejection.
function hasRepeatedBlock(s: string): boolean {
  // Half, not a third: "noreply2026noreply2026" is the block repeated TWICE, and a password like
  // that is as weak as the block alone — this was the case that slipped through the first test.
  for (let len = 1; len <= Math.floor(s.length / 2); len++) {
    const block = s.slice(0, len);
    if (block.repeat(Math.ceil(s.length / len)).slice(0, s.length) === s) return true;
  }
  // Also catches repetitions in the middle ("xyzabcabcabc").
  return /(.{2,8})\1{1,}/.test(s);
}

// Sequential run of 4+ (ascending or descending) on the keyboard or the alphabet.
function hasSequence(s: string): boolean {
  const low = s.toLowerCase();
  for (const seq of SEQUENCES) {
    const rev = [...seq].reverse().join("");
    for (let i = 0; i + 4 <= seq.length; i++) {
      if (low.includes(seq.slice(i, i + 4)) || low.includes(rev.slice(i, i + 4))) return true;
    }
  }
  return false;
}

/**
 * measure evaluates the password. `userInputs` are data from whoever is choosing it (email, name,
 * domain): zxcvbn penalizes them and Stalwart passes them along, so we penalize them too.
 */
export function measure(password: string, userInputs: string[] = []): Strength {
  const pw = password ?? "";
  if (pw.length === 0) return { score: 0, ok: false, hint: "" };
  if (pw.length < MIN_LENGTH) return { score: pw.length < 8 ? 0 : 1, ok: false, hint: "pwHintShort" };

  const low = pw.toLowerCase();
  // Fragments of the address/name: "dener", "beskos", the domain... Words of 3+ letters count.
  const parts = userInputs
    .flatMap((s) => (s ?? "").toLowerCase().split(/[^a-z0-9]+/i))
    .filter((s) => s.length >= 3);
  if (parts.some((p) => low.includes(p))) return { score: 1, ok: false, hint: "pwHintPersonal" };
  if (COMMON.some((c) => low.includes(c))) return { score: 1, ok: false, hint: "pwHintCommon" };
  if (hasRepeatedBlock(pw)) return { score: 1, ok: false, hint: "pwHintRepeat" };
  if (hasSequence(pw)) return { score: 1, ok: false, hint: "pwHintSequence" };

  // Variety + length. A long passphrase counts as much as a short mess of symbols — that's what
  // zxcvbn concludes too.
  const classes = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^a-zA-Z0-9]/].filter((re) => re.test(pw)).length;
  const words = pw.trim().split(/[\s._-]+/).filter((w) => w.length >= 3).length;
  let score: Score = 2;
  if (pw.length >= 16 || (pw.length >= 14 && classes >= 3) || words >= 4) score = 4;
  else if (pw.length >= 14 || classes >= 3 || words >= 3) score = 3;

  const hint = score >= 3 ? "" : "pwHintWeak";
  return { score, ok: score >= 3, hint };
}
