import { Metadata } from '@rapidaai/react';
import { getOptionValue, buildDefaultMetadata } from '../common';

// ============================================================================
// Constants
// ============================================================================

export const SEPARATOR = '<|||>';
const REQUIRED_KEYS = ['tool.transfer_to'];

// ============================================================================
// Default Options
// ============================================================================

export const GetTransferCallDefaultOptions = (
  current: Metadata[],
): Metadata[] =>
  buildDefaultMetadata(
    current,
    [
      { key: 'tool.transfer_to' },
      { key: 'tool.transfer_message' },
      { key: 'tool.transfer_delay', defaultValue: '0' },
    ],
    REQUIRED_KEYS,
  );

// ============================================================================
// Validation
// ============================================================================

export const ValidateTransferCallDefaultOptions = (
  options: Metadata[],
): string | undefined => {
  const transferTo = getOptionValue(options, 'tool.transfer_to');
  if (!transferTo || !transferTo.trim()) {
    return 'Please provide at least one phone number or SIP URI to transfer calls to.';
  }
  return undefined;
};
