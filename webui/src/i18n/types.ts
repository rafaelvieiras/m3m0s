/** A translation table with exactly the keys of the English source. */
export type Messages<T> = { [K in keyof T]: string };
