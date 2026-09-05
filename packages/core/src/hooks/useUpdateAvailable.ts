import { useQuery } from '@tanstack/react-query';
import { getClientVersion } from '../api/jellyfinClient';
import { getLatestRelease, type LatestRelease } from '../api/releases';
import { compareVersions } from '../utils/compareVersions';

export interface UpdateAvailability {
    /** True when the latest published release is newer than the running version */
    updateAvailable: boolean;
    /** Version of the latest published release, if it could be fetched */
    latestVersion?: string;
    /** Link to the release page on GitHub, if it could be fetched */
    releaseUrl?: string;
}

/**
 * Checks GitHub for a release newer than the running version. Failures are
 * swallowed, since instances can be offline or rate limited and a missing
 * update notice is better than an error.
 */
export function useUpdateAvailable(enabled = true): UpdateAvailability {
    const { data } = useQuery<LatestRelease>({
        queryKey: ['latest-release'],
        queryFn: getLatestRelease,
        enabled,
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: 6 * 60 * 60 * 1000,
        gcTime: 24 * 60 * 60 * 1000,
    });

    // enabled only stops the fetch; cached data would still be returned, so the
    // gate has to guard the result too.
    if (!enabled || !data?.version) return { updateAvailable: false };

    return {
        updateAvailable: compareVersions(data.version, getClientVersion()) > 0,
        latestVersion: data.version,
        releaseUrl: data.url,
    };
}
