import type { FC } from 'react';
import React from 'react';
import { ReactSortable } from 'react-sortablejs';
import { TextInput } from '@/app/components/carbon/form';
import { TertiaryButton } from '@/app/components/carbon/button';
import { Add, TrashCan, Draggable } from '@carbon/icons-react';
import { Button } from '@carbon/react';

export type Options = string[];
export type IConfigSelectProps = {
  placeholder?: string;
  label?: string;
  helperText?: string;
  options: Options;
  onChange: (options: Options) => void;
};

const ConfigSelect: FC<IConfigSelectProps> = ({
  placeholder,
  label = 'Add option',
  helperText,
  options,
  onChange,
}) => {
  const optionList = options.map((content, index) => ({
    id: index,
    name: content,
  }));

  return (
    <div>
      {options.length > 0 && (
        <table className="w-full border-collapse border border-gray-200 dark:border-gray-700 text-sm [&_input]:!border-none [&_.cds--text-input]:!border-none [&_.cds--text-input]:!outline-none [&_.cds--form-item]:!m-0">
          <ReactSortable
            tag="tbody"
            list={optionList}
            setList={list => onChange(list.map(item => item.name))}
            handle=".handle"
            ghostClass="opacity-60"
            animation={150}
          >
            {options.map((option, index) => (
              <tr
                key={optionList[index].id}
                className="border-b border-gray-200 dark:border-gray-700 last:border-b-0"
              >
                <td className="w-8 p-0 text-center border-r border-gray-200 dark:border-gray-700">
                  <span className="handle cursor-grab inline-flex items-center justify-center p-2 text-gray-400">
                    <Draggable size={16} />
                  </span>
                </td>
                <td className="p-0 border-r border-gray-200 dark:border-gray-700">
                  <TextInput
                    id={`suggestion-${index}`}
                    labelText=""
                    hideLabel
                    value={option || ''}
                    placeholder={placeholder}
                    size="md"
                    onChange={e => {
                      onChange(
                        options.map((item, i) =>
                          i === index ? e.target.value : item,
                        ),
                      );
                    }}
                  />
                </td>
                <td className="w-10 p-0 text-center">
                  <Button
                    hasIconOnly
                    renderIcon={TrashCan}
                    iconDescription="Remove"
                    kind="ghost"
                    size="sm"
                    onClick={() =>
                      onChange(options.filter((_, i) => i !== index))
                    }
                  />
                </td>
              </tr>
            ))}
          </ReactSortable>
        </table>
      )}
      <div className="pt-4">
        <TertiaryButton
          size="md"
          renderIcon={Add}
          onClick={() => onChange([...options, ''])}
          className="!w-full !max-w-none"
        >
          {label}
        </TertiaryButton>
      </div>
      {helperText && (
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
          {helperText}
        </p>
      )}
    </div>
  );
};

export default React.memo(ConfigSelect);
