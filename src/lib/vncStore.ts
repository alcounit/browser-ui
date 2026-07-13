export const NAME_PREFIX = "vnc.pw.name.";
export const VERSION_PREFIX = "vnc.pw.ver.";

export function getSavedByName(name: string, storage: Storage = localStorage): string | null {
  if (!name) return null;
  return storage.getItem(NAME_PREFIX + name);
}

export function setSavedByName(name: string, password: string, storage: Storage = localStorage): void {
  if (!name) return;
  storage.setItem(NAME_PREFIX + name, password);
}

export function versionKey(name: string, version: string): string {
  return `${name}@${version}`;
}

export function getSavedByVersion(name: string, version: string, storage: Storage = localStorage): string | null {
  if (!name || !version) return null;
  return storage.getItem(VERSION_PREFIX + versionKey(name, version));
}

export function setSavedByVersion(name: string, version: string, password: string, storage: Storage = localStorage): void {
  if (!name || !version) return;
  storage.setItem(VERSION_PREFIX + versionKey(name, version), password);
}
