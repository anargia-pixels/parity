/** Successful result with data and no error. */
type Success<T> = {
  data: T;
  error: null;
};

/** Failed result with error and no data. */
type Failure<E> = {
  data: null;
  error: E;
};

/** Discriminated union representing either a success or failure outcome. */
type Result<T, E = Error> = Success<T> | Failure<E>;

/**
 * Wraps an async promise in a try-catch, returning a discriminated union
 * so callers can handle errors without a try/catch block.
 *
 * @param promise - The async operation to wrap.
 * @returns An object with either `data` (on success) or `error` (on failure).
 */
export async function tryCatch<T, E = Error>(
  promise: Promise<T>,
): Promise<Result<T, E>> {
  try {
    const data = await promise;
    return { data, error: null };
  } catch (error) {
    return { data: null, error: error as E };
  }
}

/**
 * Wrap a synchronous function that may throw, deferring execution so the
 * throw lands inside tryCatch's await.
 */
export async function tryCatchSync<T, E = Error>(fn: () => T): Promise<Result<T, E>> {
  return tryCatch<T, E>(Promise.resolve().then(fn));
}
