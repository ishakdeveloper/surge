import { Option } from "effect";

/**
 * How each class the trip service prices is named to a rider, on both apps.
 *
 * A quote carries only the class's slug, so the names and seats are restated
 * here from the catalogue in `backend/services/trip/internal/domain/pricing.go`.
 * A slug this table does not know is shown as itself rather than hidden: a new
 * class added to the catalogue still books, it just reads plainly until it is
 * named here.
 */
export interface RideClass {
  readonly label: string;
  readonly seats: Option.Option<number>;
}

const CLASSES: Readonly<Record<string, RideClass>> = {
  sedan: { label: "Surge", seats: Option.some(4) },
  van: { label: "Surge XL", seats: Option.some(6) },
  luxury: { label: "Surge Black", seats: Option.some(4) },
};

export const rideClass = (slug: string): RideClass =>
  CLASSES[slug] ?? { label: slug, seats: Option.none() };
