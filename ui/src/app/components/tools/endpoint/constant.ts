import { Metadata } from '@rapidaai/react';
import { getOptionValue, buildDefaultMetadata } from '../common';

// ============================================================================
// Constants
// ============================================================================

const REQUIRED_KEYS = ['tool.endpoint_id', 'tool.parameters'];

// ============================================================================
// Default Options
// ============================================================================

export const GetEndpointDefaultOptions = (current: Metadata[]): Metadata[] =>
  buildDefaultMetadata(
    current,
    [{ key: 'tool.endpoint_id' }, { key: 'tool.parameters', defaultValue: '{"tool.argument":"argument"}' }],
    REQUIRED_KEYS,
  );

// ============================================================================
// Validation
// ============================================================================

const validateRequiredKeys = (options: Metadata[]): string | undefined => {
  const foundKeys = new Set(options.map(opt => opt.getKey()));
  const missingKeys = REQUIRED_KEYS.filter(key => !foundKeys.has(key));
  if (missingKeys.length > 0) {
    return `Missing required metadata keys: ${missingKeys.join(', ')}.`;
  }
  return undefined;
};

const validateEndpointId = (value: string | undefined): string | undefined => {
  if (typeof value !== 'string' || value === '') {
    return 'Please provide a valid endpoint ID.';
  }
  return undefined;
};

const validateParameters = (value: string | undefined): string | undefined => {
  if (typeof value !== 'string' || value === '') {
    return 'Please provide valid parameters as a non-empty JSON string.';
  }
  try {
    const parameters = JSON.parse(value);
    if (
      typeof parameters !== 'object' ||
      parameters === null ||
      Array.isArray(parameters)
    ) {
      return 'Parameters must be a valid JSON object.';
    }
    const entries = Object.entries(parameters);
    if (entries.length === 0) {
      return 'Parameters cannot be an empty object.';
    }
    for (const [paramKey, paramValue] of entries) {
      const [type, key] = paramKey.split('.');
      if (!type || !key || typeof paramValue !== 'string' || paramValue === '') {
        return 'Each parameter key must follow the "type.key" format with a non-empty string value.';
      }
    }
    const values = entries.map(([, v]) => v);
    if (new Set(values).size !== values.length) {
      return 'Parameter values must be unique.';
    }
  } catch {
    return 'Please provide a valid JSON string for parameters.';
  }
  return undefined;
};

export const ValidateEndpointDefaultOptions = (
  options: Metadata[],
): string | undefined =>
  validateRequiredKeys(options) ||
  validateEndpointId(getOptionValue(options, 'tool.endpoint_id')) ||
  validateParameters(getOptionValue(options, 'tool.parameters'));
