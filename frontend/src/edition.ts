import type { ComponentType } from "react";
import type { Me } from "./api";

export const hostedBilling = false;
export const hostedPages: {
  signup: ComponentType | null;
  billing: ComponentType<{ me: Me | undefined; detail?: string; onDetail?: (id: string) => void }> | null;
} = { signup: null, billing: null };
