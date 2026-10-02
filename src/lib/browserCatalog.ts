export type SupportedBrowsers = Record<string, Record<string, string[]>>;

export interface BrowserEntry {
  name: string;
  versions: string[];
}

export interface SessionGroup {
  sessionType: string;
  browsers: BrowserEntry[];
}

const STARTABLE_TYPES = new Set(["selenium"]);

export const canStart = (sessionType: string): boolean => STARTABLE_TYPES.has(sessionType);

export const creatableGroups = (groups: SessionGroup[]): SessionGroup[] =>
  groups.filter((group) => canStart(group.sessionType));

export const startBrowserError = async (res: Response): Promise<Error> => {
  let reason = "";

  try {
    const payload = await res.json();
    reason = payload?.reason || payload?.error || "";
  } catch {
    reason = "";
  }

  return new Error(reason || res.statusText || "Failed to start browser");
};

export const buildCatalog = (cfgs: SupportedBrowsers[]): SessionGroup[] => {
  const groups = new Map<string, Map<string, Set<string>>>();

  cfgs.forEach((cfg) => {
    Object.entries(cfg ?? {}).forEach(([sessionType, browsers]) => {
      let group = groups.get(sessionType);
      if (!group) {
        group = new Map<string, Set<string>>();
        groups.set(sessionType, group);
      }

      Object.entries(browsers ?? {}).forEach(([name, versions]) => {
        let collected = group!.get(name);
        if (!collected) {
          collected = new Set<string>();
          group!.set(name, collected);
        }
        (versions ?? []).forEach((version) => collected!.add(version));
      });
    });
  });

  return [...groups.entries()]
    .map(([sessionType, browsers]) => ({
      sessionType,
      browsers: [...browsers.entries()]
        .map(([name, versions]) => ({
          name,
          versions: [...versions].sort((a, b) => b.localeCompare(a, undefined, { numeric: true })),
        }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.sessionType.localeCompare(b.sessionType));
};

export const flattenBrowsers = (groups: SessionGroup[]): BrowserEntry[] => {
  const merged = new Map<string, Set<string>>();

  groups.forEach((group) => {
    group.browsers.forEach((browser) => {
      let versions = merged.get(browser.name);
      if (!versions) {
        versions = new Set<string>();
        merged.set(browser.name, versions);
      }
      browser.versions.forEach((version) => versions!.add(version));
    });
  });

  return [...merged.entries()]
    .map(([name, versions]) => ({
      name,
      versions: [...versions].sort((a, b) => b.localeCompare(a, undefined, { numeric: true })),
    }))
    .sort((a, b) => a.name.localeCompare(b.name));
};
