-- A driver's own trips, for a driver app that reloads mid-ride and has to find
-- the trip it is on.
--
-- Partial, because most rows that will ever exist are simulator trips and
-- unmatched requests with no driver at all, and indexing a column that is null
-- for them buys nothing but write cost on the path that books a ride.
create index if not exists trip_driver_created_at
  on trip (driver_id, created_at desc)
  where driver_id is not null and driver_id <> '';
