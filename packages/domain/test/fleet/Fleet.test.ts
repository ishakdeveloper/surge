import { DocumentId, UserId, VehicleId } from "@surge/domain/api/Primitives";
import {
  type Document,
  documentOf,
  type Driver,
  expiresWithin,
  isApproved,
  isBlocked,
  isUsable,
  isValid,
  nextStep,
  readDate,
  type Vehicle,
} from "@surge/domain/fleet/Fleet";
import { describe, expect, it } from "vitest";

const now = new Date("2026-09-12T12:00:00Z");
const inAYear = "2027-09-12T00:00:00Z";
const lastMonth = "2026-08-12T00:00:00Z";

const driver = (over: Partial<Driver> = {}): Driver => ({
  driverId: UserId.make("drv-1"),
  status: "DRIVER_STATUS_ONBOARDING",
  blockedReason: "",
  identity: "IDENTITY_STATUS_UNSTARTED",
  verifiedName: "",
  licenceExpiresAt: "",
  outstanding: [],
  approvedAt: "",
  createdAt: now.toISOString(),
  updatedAt: now.toISOString(),
  ...over,
});

const vehicle = (over: Partial<Vehicle> = {}): Vehicle => ({
  id: VehicleId.make("veh-1"),
  driverId: UserId.make("drv-1"),
  plate: "02JLT3",
  make: "Toyota",
  model: "Toyota Prius",
  colour: "Wit",
  seats: 5,
  packageSlug: "sedan",
  status: "VEHICLE_STATUS_APPROVED",
  rejectedReason: "",
  apkExpiresAt: inAYear,
  taxiRegistered: true,
  insured: true,
  firstRegisteredAt: "2018-04-01T00:00:00Z",
  registerCheckedAt: now.toISOString(),
  createdAt: now.toISOString(),
  updatedAt: now.toISOString(),
  ...over,
});

const document = (over: Partial<Document> = {}): Document => ({
  id: DocumentId.make("doc-1"),
  driverId: UserId.make("drv-1"),
  vehicleId: VehicleId.make(""),
  kind: "DOCUMENT_KIND_INSURANCE",
  status: "DOCUMENT_STATUS_APPROVED",
  expiresAt: inAYear,
  extracted: {
    status: "EXTRACTION_STATUS_NONE",
    insurer: "",
    policyNumber: "",
    expiresAt: "",
    quotes: [],
  },
  reviewNote: "",
  reviewedAt: "",
  createdAt: now.toISOString(),
  updatedAt: now.toISOString(),
  ...over,
});

describe("a driver's standing", () => {
  it("is approved only when the server says so", () => {
    expect(isApproved(driver({ status: "DRIVER_STATUS_APPROVED" }))).toBe(true);
    expect(isApproved(driver())).toBe(false);
    expect(isBlocked(driver({ status: "DRIVER_STATUS_BLOCKED" }))).toBe(true);
  });

  /**
   * The server orders what is outstanding, so the first is the one to put in
   * front of the driver. A screen that asked for all of them at once would be
   * a form rather than a path through one.
   */
  it("has one next step, in the server's order", () => {
    const onboarding = driver({
      outstanding: [
        { kind: "REQUIREMENT_KIND_IDENTITY", detail: "Verify your identity." },
        { kind: "REQUIREMENT_KIND_VEHICLE", detail: "Add a car." },
      ],
    });
    expect(nextStep(onboarding)?.kind).toBe("REQUIREMENT_KIND_IDENTITY");
    expect(nextStep(driver())).toBeUndefined();
  });
});

describe("cars and papers", () => {
  it("count only while their dates hold", () => {
    expect(isUsable(vehicle(), now)).toBe(true);
    // Approved months ago, inspection lapsed since: not a car to dispatch.
    expect(isUsable(vehicle({ apkExpiresAt: lastMonth }), now)).toBe(false);
    expect(isUsable(vehicle({ status: "VEHICLE_STATUS_PENDING" }), now)).toBe(false);

    expect(isValid(document(), now)).toBe(true);
    expect(isValid(document({ expiresAt: lastMonth }), now)).toBe(false);
    expect(isValid(document({ status: "DOCUMENT_STATUS_SUBMITTED" }), now)).toBe(false);
    // A registration has no date at all and counts until somebody says otherwise.
    expect(isValid(document({ expiresAt: "" }), now)).toBe(true);
  });

  /** Worth telling a driver before it stops them working. */
  it("can be seen running out", () => {
    expect(expiresWithin("2026-10-01T00:00:00Z", now, 30)).toBe(true);
    expect(expiresWithin(inAYear, now, 30)).toBe(false);
    // Already gone is not "running out"; it is a different thing to say.
    expect(expiresWithin(lastMonth, now, 30)).toBe(false);
    expect(expiresWithin("", now, 30)).toBe(false);
  });

  it("are found by kind, and by the car they belong to", () => {
    const papers = [
      document({ id: DocumentId.make("doc-vog"), kind: "DOCUMENT_KIND_VOG" }),
      document({ id: DocumentId.make("doc-ins"), vehicleId: VehicleId.make("veh-1") }),
    ];
    expect(documentOf(papers, "DOCUMENT_KIND_VOG")?.id).toBe("doc-vog");
    expect(documentOf(papers, "DOCUMENT_KIND_INSURANCE", VehicleId.make("veh-1"))?.id).toBe(
      "doc-ins",
    );
    // The insurance for a car the driver no longer has is not this car's.
    expect(documentOf(papers, "DOCUMENT_KIND_INSURANCE", VehicleId.make("veh-2"))).toBeUndefined();
  });

  /** Every unset date on this API is an empty string, not an absent field. */
  it("read an unset date as nothing", () => {
    expect(readDate("")).toBeUndefined();
    expect(readDate("not a date")).toBeUndefined();
    expect(readDate(inAYear)?.getUTCFullYear()).toBe(2027);
  });
});
