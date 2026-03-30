import { FC, useEffect, useState } from 'react';
import { TextInput } from '@/app/components/carbon/form';
import { TertiaryButton } from '@/app/components/carbon/button';
import { Add, TrashCan } from '@carbon/icons-react';
import { Button } from '@carbon/react';

interface Header {
  key: string;
  value: string;
}

export const APiHeader: FC<{
  inputClass?: string;
  headers: Header[];
  setHeaders: (headers: Header[]) => void;
}> = ({ headers, setHeaders }) => {
  const updateHeader = (
    index: number,
    field: 'key' | 'value',
    value: string,
  ) => {
    const updatedHeaders = [...headers];
    updatedHeaders[index][field] = value;
    setHeaders(updatedHeaders);
  };

  return (
    <>
      <table className="w-full border-collapse border border-gray-200 dark:border-gray-700 text-sm [&_input]:!border-none [&_.cds--text-input]:!border-none [&_.cds--text-input]:!outline-none [&_.cds--form-item]:!m-0">
        <thead>
          <tr className="bg-gray-50 dark:bg-gray-900">
            <th className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 px-3 py-2 border-b border-r border-gray-200 dark:border-gray-700 w-1/2">Key</th>
            <th className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 px-3 py-2 border-b border-r border-gray-200 dark:border-gray-700 w-1/2">Value</th>
            <th className="border-b border-gray-200 dark:border-gray-700 w-10" />
          </tr>
        </thead>
        <tbody>
          {headers.map((header, index) => (
            <tr key={index} className="border-b border-gray-200 dark:border-gray-700 last:border-b-0">
              <td className="border-r border-gray-200 dark:border-gray-700 p-0">
                <TextInput id={`api-header-key-${index}`} labelText="" hideLabel value={header.key} onChange={e => updateHeader(index, 'key', e.target.value)} placeholder="Key" size="md" />
              </td>
              <td className="border-r border-gray-200 dark:border-gray-700 p-0">
                <TextInput id={`api-header-val-${index}`} labelText="" hideLabel value={header.value} onChange={e => updateHeader(index, 'value', e.target.value)} placeholder="Value" size="md" />
              </td>
              <td className="p-0 text-center">
                <Button hasIconOnly renderIcon={TrashCan} iconDescription="Remove" kind="danger--ghost" size="sm" onClick={() => setHeaders(headers.filter((_, i) => i !== index))} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="pt-4">
        <TertiaryButton
          size="md"
          renderIcon={Add}
          onClick={() => setHeaders([...headers, { key: '', value: '' }])}
          className="!w-full !max-w-none"
        >
          Add header
        </TertiaryButton>
      </div>
    </>
  );
};

export const APiStringHeader: FC<{
  inputClass?: string;
  headerValue?: string;
  setHeaderValue: (s: string) => void;
}> = ({ headerValue = '{}', setHeaderValue }) => {
  const [headers, setHeaders] = useState<Header[]>([{ key: '', value: '' }]);

  useEffect(() => {
    try {
      const parsedHeaders = JSON.parse(headerValue);
      const headerArray = Object.entries(parsedHeaders).map(([key, value]) => ({
        key,
        value: value as string,
      }));
      setHeaders(headerArray);
    } catch (error) {
      console.error('Error parsing header JSON:', error);
    }
  }, [headerValue]);

  const handleSetHeaders = (updatedHeaders: Header[]) => {
    setHeaders(updatedHeaders);
    const headersObject = updatedHeaders.reduce(
      (acc, header) => {
        if (header.key) acc[header.key] = header.value;
        return acc;
      },
      {} as Record<string, string>,
    );
    setHeaderValue(JSON.stringify(headersObject));
  };

  return (
    <APiHeader
      headers={headers}
      setHeaders={handleSetHeaders}
    />
  );
};
