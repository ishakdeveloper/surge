import type { SimulatorGet200 } from "../api/SurgeApi.js";

/**
 * The simulator's configuration and what it is actually running, named once
 * from the generated client rather than restated, like `Trip`.
 */
export type Simulator = SimulatorGet200["simulator"];
