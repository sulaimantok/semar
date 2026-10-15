import type { ReactNode } from "react";

import { cn } from "@/shared/utils/cn";

import { formFieldStyles as styles } from "./form-field.styles";

export interface IFormFieldProps {
  label: string;
  hint?: string;
  optional?: boolean;
  wide?: boolean;
  /** The input; it is wrapped in the label, so no htmlFor is needed. */
  children: ReactNode;
}

export function FormField({ label, hint, optional, wide, children }: IFormFieldProps) {
  return (
    <label className={cn(styles.field, wide && styles.wide)}>
      <span className={styles.label}>
        {label}
        {optional && <span className={styles.optional}>(optional)</span>}
      </span>
      {children}
      {hint && <span className={styles.hint}>{hint}</span>}
    </label>
  );
}
