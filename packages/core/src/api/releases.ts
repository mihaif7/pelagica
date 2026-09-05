const LATEST_RELEASE_URL = 'https://api.github.com/repos/PelagicaApp/pelagica/releases/latest';

export interface LatestRelease {
    /** Release version, e.g. "4.8.0" */
    version: string;
    /** Link to the release page on GitHub */
    url: string;
}

/**
 * Fetches the latest published release from GitHub. Prereleases and drafts are
 * excluded by the endpoint itself.
 */
export const getLatestRelease = async (): Promise<LatestRelease> => {
    const res = await fetch(LATEST_RELEASE_URL, {
        headers: { Accept: 'application/vnd.github+json' },
    });
    if (!res.ok) {
        throw new Error('Failed to fetch latest release');
    }
    const data = await res.json();
    return {
        version: String(data.tag_name ?? '').replace(/^v/, ''),
        url: String(data.html_url ?? ''),
    };
};
