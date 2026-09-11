import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** shadcn's class merger, as in the web app: conditional classes, later utilities winning. */
export function cn(...inputs: Array<ClassValue>) {
  return twMerge(clsx(inputs));
}
