import { Metadata } from '@rapidaai/react';
import { useCallback, useMemo } from 'react';
import { KeyValueParameter } from './types';

/**
 * Hook for managing Metadata parameters with get/set operations
 */
export const useParameterManager = (
  parameters: Metadata[] | null,
  onParameterChange: (params: Metadata[]) => void,
) => {
  const getParamValue = useCallback(
    (key: string): string => {
      return parameters?.find(p => p.getKey() === key)?.getValue() ?? '';
    },
    [parameters],
  );

  const updateParameter = useCallback(
    (key: string, value: string) => {
      const updatedParams = [...(parameters || [])];
      const existingIndex = updatedParams.findIndex(p => p.getKey() === key);

      const newParam = new Metadata();
      newParam.setKey(key);
      newParam.setValue(value);

      if (existingIndex >= 0) {
        updatedParams[existingIndex] = newParam;
      } else {
        updatedParams.push(newParam);
      }

      onParameterChange(updatedParams);
    },
    [parameters, onParameterChange],
  );

  return { getParamValue, updateParameter };
};

/**
 * Parses a JSON string into key-value parameters array
 */
export const parseJsonParameters = (
  jsonString: string,
): KeyValueParameter[] => {
  try {
    const parsed = JSON.parse(jsonString);
    return Object.entries(parsed).map(([key, value]) => ({
      key,
      value: value as string,
    }));
  } catch {
    return [];
  }
};

/**
 * Converts key-value parameters array to JSON string
 */
export const stringifyParameters = (params: KeyValueParameter[]): string => {
  const validParams = params.filter(({ key, value }) => {
    const [type, pk] = key.split('.');
    return type && pk && value;
  });
  return JSON.stringify(
    Object.fromEntries(validParams.map(({ key, value }) => [key, value])),
  );
};

/**
 * Hook for managing JSON-based key-value parameters with local state sync
 */
export const useKeyValueParameters = (
  initialValue: string,
  onChange: (value: string) => void,
) => {
  const initialParams = useMemo(
    () => parseJsonParameters(initialValue),
    [initialValue],
  );

  return {
    initialParams,
    updateAndSync: (newParams: KeyValueParameter[]) => {
      onChange(stringifyParameters(newParams));
      return newParams;
    },
  };
};
