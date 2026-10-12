import type { components } from "./schema.gen";

export type IProblem = components["schemas"]["Problem"];

/** Error thrown by services when semar-api answers with an RFC 9457 problem. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem?: IProblem;

  constructor(status: number, problem?: IProblem) {
    super(problem?.detail ?? problem?.title ?? `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }

  /** Correlation ID to quote when reporting the error. */
  get requestId(): string | undefined {
    return this.problem?.requestId;
  }
}

/** Narrows an unknown error body to a problem object. */
export function isProblem(value: unknown): value is IProblem {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as IProblem).status === "number" &&
    typeof (value as IProblem).title === "string"
  );
}
