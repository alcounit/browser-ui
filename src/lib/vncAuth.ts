export const MAX_AUTH_ATTEMPTS = 5;

export interface AttemptState {
  tried: Set<string>;
  attempts: number;
  manual: boolean;
}

export function newAttemptState(): AttemptState {
  return { tried: new Set<string>(), attempts: 0, manual: false };
}

export function buildLadder(...candidates: Array<string | null | undefined>): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const candidate of candidates) {
    if (!candidate) continue;
    if (seen.has(candidate)) continue;
    seen.add(candidate);
    out.push(candidate);
  }
  return out;
}

export function nextCandidate(ladder: string[], tried: Set<string>): string | null {
  for (const password of ladder) {
    if (!tried.has(password)) return password;
  }
  return null;
}

export function registerFailure(state: AttemptState, password: string): AttemptState {
  const tried = new Set(state.tried);
  tried.add(password);
  return { ...state, tried, attempts: state.attempts + 1 };
}

export function markManual(state: AttemptState): AttemptState {
  return { ...state, manual: true };
}

export function isReused(state: AttemptState, password: string): boolean {
  return state.tried.has(password);
}

export function isHardCapReached(state: AttemptState, max: number = MAX_AUTH_ATTEMPTS): boolean {
  return state.attempts >= max;
}

export function shouldPrompt(state: AttemptState, ladder: string[], max: number = MAX_AUTH_ATTEMPTS): boolean {
  if (isHardCapReached(state, max)) return false;
  return nextCandidate(ladder, state.tried) === null;
}
