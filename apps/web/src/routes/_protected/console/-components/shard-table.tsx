import { fleetAtom } from "@/atom/console-atoms.js";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table.js";
import { cn } from "@/lib/utils.js";
import { useAtomValue } from "@effect/atom-react";
import { AsyncResult } from "effect/unstable/reactivity";

/** A frame this old means the partition's owner died or is mid-handover. */
const STALE_MS = 2000;

/**
 * How the city is divided: each Kafka partition, the matcher that owns it, and
 * what it is holding. Kill a matcher and watch its rows go stale and then move.
 */
export const ShardTable = () => {
  const fleet = useAtomValue(fleetAtom);
  if (!AsyncResult.isSuccess(fleet)) return null;
  const { shards } = fleet.value.latest;
  const instances = new Set(shards.map((shard) => shard.instance)).size;

  return (
    <section className="flex flex-col gap-3" aria-labelledby="shards">
      <h2 id="shards" className="text-sm font-medium">Shards</h2>
      <p className="text-muted-foreground text-sm">
        {shards.length} partitions reporting, across {instances}{" "}
        {instances === 1 ? "matcher" : "matchers"}.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Partition</TableHead>
            <TableHead>Matcher</TableHead>
            <TableHead className="text-right">Drivers</TableHead>
            <TableHead className="text-right">Waiting</TableHead>
            <TableHead className="text-right">Age</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {shards.map((shard) => (
            <TableRow key={shard.partition}>
              <TableCell className="tabular-nums">{shard.partition}</TableCell>
              <TableCell className="max-w-28 truncate font-mono text-xs">
                {shard.instance}
              </TableCell>
              <TableCell className="text-right tabular-nums">{shard.drivers}</TableCell>
              <TableCell className="text-right tabular-nums">{shard.pending}</TableCell>
              <TableCell
                className={cn(
                  "text-right tabular-nums",
                  shard.ageMs > STALE_MS && "text-destructive",
                )}
              >
                {(shard.ageMs / 1000).toFixed(1)}s
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  );
};
