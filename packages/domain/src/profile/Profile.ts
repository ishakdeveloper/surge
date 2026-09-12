import type { ProfilesGetMine200 } from "../api/SurgeApi.js";

/**
 * Who someone is to the people they ride with: a first name and a link to a
 * photo, each empty until given. Named from the generated client, as
 * `../trip/Trip.ts` names its shapes, so a field that moves in
 * `proto/profile.proto` moves here on the next `make proto`.
 */
export type Profile = ProfilesGetMine200["profile"];
