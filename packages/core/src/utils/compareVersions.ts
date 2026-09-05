const parseVersion = (version: string): number[] =>
    version
        .trim()
        .replace(/^v/, '')
        .split('-')[0]
        .split('.')
        .map((part) => Number.parseInt(part, 10));

/**
 * Compares two dot separated versions, returning a negative number when a is
 * older than b, a positive number when it is newer, and 0 when they match.
 * Prerelease suffixes are ignored, invalid parts are treated as 0.
 */
export function compareVersions(a: string, b: string): number {
    const left = parseVersion(a);
    const right = parseVersion(b);

    for (let i = 0; i < Math.max(left.length, right.length); i++) {
        const diff = (left[i] || 0) - (right[i] || 0);
        if (diff !== 0) return diff;
    }

    return 0;
}
