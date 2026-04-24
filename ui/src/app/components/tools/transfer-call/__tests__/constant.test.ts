import { Metadata } from '@rapidaai/react';
import {
  GetTransferCallDefaultOptions,
  ValidateTransferCallDefaultOptions,
} from '../constant';

const createMetadata = (key: string, value: string): Metadata => {
  const metadata = new Metadata();
  metadata.setKey(key);
  metadata.setValue(value);
  return metadata;
};

describe('transfer-call default options', () => {
  it('keeps transfer message and delay fields when existing values are provided', () => {
    const options = GetTransferCallDefaultOptions([
      createMetadata('tool.transfer_to', '+14155551234'),
      createMetadata('tool.transfer_message', 'Hold on while I transfer.'),
      createMetadata('tool.transfer_delay', '200'),
    ]);
    const keySet = new Set(options.map(option => option.getKey()));

    expect(keySet.has('tool.transfer_to')).toBe(true);
    expect(keySet.has('tool.transfer_message')).toBe(true);
    expect(keySet.has('tool.transfer_delay')).toBe(true);
  });

  it('preserves existing transfer message and delay values', () => {
    const options = GetTransferCallDefaultOptions([
      createMetadata('tool.transfer_to', '+14155551234'),
      createMetadata('tool.transfer_message', 'Please hold while I transfer you.'),
      createMetadata('tool.transfer_delay', '350'),
    ]);

    const byKey = Object.fromEntries(
      options.map(option => [option.getKey(), option.getValue()]),
    );

    expect(byKey['tool.transfer_to']).toBe('+14155551234');
    expect(byKey['tool.transfer_message']).toBe(
      'Please hold while I transfer you.',
    );
    expect(byKey['tool.transfer_delay']).toBe('350');
  });

  it('validates transfer destination as required', () => {
    const error = ValidateTransferCallDefaultOptions([
      createMetadata('tool.transfer_message', 'message'),
      createMetadata('tool.transfer_delay', '100'),
    ]);

    expect(error).toContain('Please provide at least one phone number');
  });
});
