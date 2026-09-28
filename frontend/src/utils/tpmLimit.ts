export function validateTpmLimit(value: unknown): asserts value is number | undefined {
  if (value !== undefined && (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0)) {
    throw new Error('tpm_limit must be a non-negative safe integer')
  }
}
