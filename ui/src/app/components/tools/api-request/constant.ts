import { Metadata } from '@rapidaai/react';
import { getOptionValue, buildDefaultMetadata } from '../common';

// ============================================================================
// Constants
// ============================================================================

const REQUIRED_KEYS = ['tool.method', 'tool.endpoint', 'tool.parameters'];
const ALL_KEYS = [...REQUIRED_KEYS, 'tool.headers'];
const VALID_HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'];

// ============================================================================
// Default Options
// ============================================================================

export const GetAPIRequestDefaultOptions = (
  current: Metadata[],
): Metadata[] =>
  buildDefaultMetadata(
    current,
    [
      { key: 'tool.method', defaultValue: 'POST' },
      { key: 'tool.endpoint' },
      { key: 'tool.headers', defaultValue: '{}' },
      { key: 'tool.parameters', defaultValue: '{"tool.argument":"argument"}' },
    ],
    ALL_KEYS,
  );

// ============================================================================
// Validation
// ============================================================================

const validateRequiredKeys = (options: Metadata[]): string | undefined => {
  const missingKeys = REQUIRED_KEYS.filter(
    key => !options.some(opt => opt.getKey() === key),
  );
  if (missingKeys.length > 0) {
    return `Please provide all required metadata keys: ${REQUIRED_KEYS.join(', ')}.`;
  }
  return undefined;
};

const validateHttpMethod = (method: string | undefined): string | undefined => {
  if (method && !VALID_HTTP_METHODS.includes(method.toUpperCase())) {
    return `Invalid HTTP method. Supported methods are: ${VALID_HTTP_METHODS.join(', ')}.`;
  }
  return undefined;
};

const validateEndpoint = (endpoint: string | undefined): string | undefined => {
  if (endpoint) {
    try {
      new URL(endpoint);
    } catch {
      return 'Please provide a valid URL for the endpoint.';
    }
  }
  return undefined;
};

const validateHeaders = (headers: string | undefined): string | undefined => {
  if (!headers) return undefined;
  try {
    const parsed = JSON.parse(headers);
    for (const [key, value] of Object.entries(parsed)) {
      if (
        typeof key !== 'string' ||
        typeof value !== 'string' ||
        key.trim() === '' ||
        (value as string).trim() === ''
      ) {
        return `Invalid header entry. Keys and values must be non-empty strings.`;
      }
    }
  } catch {
    return 'Please provide valid JSON for headers.';
  }
  return undefined;
};

const validateParameters = (params: string | undefined): string | undefined => {
  if (typeof params !== 'string' || params === '') {
    return 'Please provide valid parameters as a non-empty string.';
  }
  try {
    const parsed = JSON.parse(params);
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      return 'Parameters must be a valid JSON object.';
    }
    const entries = Object.entries(parsed);
    if (entries.length === 0) {
      return 'Parameters object must contain at least one key-value pair.';
    }
    for (const [paramKey, paramValue] of entries) {
      const [type, key] = paramKey.split('.');
      if (!type || !key || typeof paramValue !== 'string' || paramValue === '') {
        return `Invalid parameter format. Key "${paramKey}" must be "type.key" and value must be a non-empty string.`;
      }
    }
    const values = entries.map(([, v]) => v);
    if (new Set(values).size !== values.length) {
      return 'Parameter values must be unique.';
    }
  } catch {
    return 'Please provide valid JSON for parameters.';
  }
  return undefined;
};

export const ValidateAPIRequestDefaultOptions = (
  options: Metadata[],
): string | undefined =>
  validateRequiredKeys(options) ||
  validateHttpMethod(getOptionValue(options, 'tool.method')) ||
  validateEndpoint(getOptionValue(options, 'tool.endpoint')) ||
  validateHeaders(getOptionValue(options, 'tool.headers')) ||
  validateParameters(getOptionValue(options, 'tool.parameters'));
