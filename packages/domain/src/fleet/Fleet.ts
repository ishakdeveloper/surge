import {
  FleetGetMyDriver200,
  type FleetStartUpload200,
  type ReviewGet200,
  type ReviewList200,
} from "../api/SurgeApi.js";

/**
 * Names for the shapes the fleet returns, and the rules about them that no
 * document can state — derived rather than restated, as `../trip/Trip.ts` is.
 */

/** A driver's standing, and what stands between them and their first trip. */
export type Driver = FleetGetMyDriver200["driver"];

/** One thing outstanding, with the sentence to show the driver. */
export type Requirement = Driver["outstanding"][number];

/** A car, as the vehicle register describes it and a reviewer decided about it. */
export type Vehicle = FleetGetMyDriver200["vehicles"][number];

/** A paper a driver owes, whether or not a file has arrived. */
export type Document = FleetGetMyDriver200["documents"][number];

/** What a machine read off a document. Advice to a reviewer, never a decision. */
export type Extraction = Document["extracted"];

/** A link to put one file to, and the document it will belong to. */
export type Upload = FleetStartUpload200;

/** One driver waiting in the review queue. */
export type ReviewItem = ReviewList200["items"][number];

/** A document with a link a reviewer can open. */
export type ReviewDocument = ReviewGet200["documents"][number];

/**
 * The protobuf enum names, verbatim, as runtime schemas lifted out of the
 * generated client — the choice `TripStatus` makes, for its reason.
 */
export const DriverStatus = FleetGetMyDriver200.fields.driver.fields.status;
export type DriverStatus = typeof DriverStatus.Type;

export const IdentityStatus = FleetGetMyDriver200.fields.driver.fields.identity;
export type IdentityStatus = typeof IdentityStatus.Type;

export type VehicleStatus = Vehicle["status"];
export type DocumentKind = Document["kind"];
export type DocumentStatus = Document["status"];
export type RequirementKind = Requirement["kind"];

/** Whether this driver may be offered work right now. */
export const isApproved = (driver: Driver): boolean => driver.status === "DRIVER_STATUS_APPROVED";

/** Whether a person has stopped them, whatever their papers say. */
export const isBlocked = (driver: Driver): boolean => driver.status === "DRIVER_STATUS_BLOCKED";

/**
 * What to ask the driver for next.
 *
 * The server orders what is outstanding, so the first is the one to put in
 * front of them; a screen that asked for all of them at once would be a form
 * rather than a path.
 */
export const nextStep = (driver: Driver): Requirement | undefined => driver.outstanding[0];

/** Whether the identity check is still to be done, or is being decided. */
export const identityPending = (driver: Driver): boolean =>
  driver.identity === "IDENTITY_STATUS_PENDING"
  || driver.identity === "IDENTITY_STATUS_PROCESSING";

/** A car that may be dispatched: approved, and its inspection still in date. */
export const isUsable = (vehicle: Vehicle, now: Date): boolean =>
  vehicle.status === "VEHICLE_STATUS_APPROVED" && !hasPassed(vehicle.apkExpiresAt, now);

/** A document that counts today. */
export const isValid = (paper: Document, now: Date): boolean =>
  paper.status === "DOCUMENT_STATUS_APPROVED" && !hasPassed(paper.expiresAt, now);

/**
 * A document or inspection running out within `days`.
 *
 * Worth showing a driver before it stops them working, which is the whole
 * reason the dates are on the client at all.
 */
export const expiresWithin = (when: string, now: Date, days: number): boolean => {
  const date = readDate(when);
  if (date === undefined) return false;
  const limit = new Date(now.getTime() + days * 24 * 60 * 60 * 1000);
  return date > now && date <= limit;
};

/** Whether a document is waiting on somebody who answers in weeks. */
export const awaitsAuthority = (paper: Document): boolean =>
  paper.status === "DOCUMENT_STATUS_AWAITING_AUTHORITY";

/** The document of a kind, for a car when the kind belongs to one. */
export const documentOf = (
  documents: ReadonlyArray<Document>,
  kind: DocumentKind,
  vehicleId = "",
): Document | undefined =>
  documents.find((paper) => paper.kind === kind && paper.vehicleId === vehicleId);

/**
 * Dates arrive as RFC 3339 strings, and empty when unset — the rule this whole
 * API follows. Reading one is the only place that has to know.
 */
export const readDate = (value: string): Date | undefined => {
  if (value === "") return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
};

const hasPassed = (value: string, now: Date): boolean => {
  const date = readDate(value);
  return date !== undefined && date <= now;
};
