import { cn } from '@/utils';
import { AnimatePresence, motion } from 'framer-motion';
import { ChevronDown } from '@carbon/icons-react';
import { FC, HTMLAttributes, useState } from 'react';

interface InputGroupProps extends HTMLAttributes<HTMLDivElement> {
  title?: any;
  initiallyExpanded?: boolean;
  childClass?: string;
}
export const InputGroup: FC<InputGroupProps> = ({
  initiallyExpanded = true,
  childClass,
  ...props
}) => {
  const [isExpanded, setIsExpanded] = useState(initiallyExpanded);

  return (
    <section
      {...props}
      className={cn('border-b border-gray-200 dark:border-gray-800', props.className)}
    >
      {/* Carbon accordion trigger */}
      <div
        onClick={() => setIsExpanded(!isExpanded)}
        className={cn(
          'h-12 px-4 flex items-center justify-between w-full cursor-pointer',
          'hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors',
          isExpanded && 'border-b border-gray-200 dark:border-gray-800',
        )}
      >
        <div className="flex-none text-sm font-semibold text-gray-900 dark:text-gray-100">
          {props.title}
        </div>
        <ChevronDown
          size={16}
          className={cn('text-gray-500 transition-transform duration-200', isExpanded && 'rotate-180')}
        />
      </div>
      <AnimatePresence>
        <motion.div
          className={cn('px-6 py-6', childClass)}
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: 'auto' }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.2, ease: 'easeInOut' }}
          style={{ display: isExpanded ? 'block' : 'none' }}
        >
          {props.children}
        </motion.div>
      </AnimatePresence>
    </section>
  );
};
