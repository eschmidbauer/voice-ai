import { useCurrentCredential } from '@/hooks/use-credential';
import { useGlobalNavigation } from '@/hooks/use-global-navigator';
import { cn } from '@/utils';
import {
  ChatBot,
  Connect,
  Folders,
  Plug,
  Debug,
  Activity,
  ArrowRight,
  Launch,
} from '@carbon/icons-react';
import { ClickableTile, Tag, Button, Link } from '@carbon/react';
import { PrimaryButton } from '@/app/components/carbon/button';

const quickStart = [
  {
    title: 'Build',
    description: 'Explore Rapida with easy starter tutorials and services.',
    tag: 'Getting started',
    route: '/deployment/assistant/create-assistant',
    featured: true,
  },
  {
    title: 'AI Assistants',
    description:
      'Build conversational AI agents with custom skills, tools, and multi-step reasoning for any channel.',
    tag: 'Popular',
    route: '/deployment/assistant',
  },
  {
    title: 'Model Integration',
    description:
      'Connect OpenAI, Anthropic, Google, and custom LLMs. Manage credentials and model configuration.',
    tag: 'Popular',
    route: '/integration/models',
  },
  {
    title: 'Knowledge Hub',
    description:
      'Upload documents, manage training data, and build RAG-powered knowledge bases for your assistants.',
    tag: 'Getting started',
    route: '/knowledge',
  },
  {
    title: 'Endpoints & Governance',
    description:
      'Secure API endpoints with fine-grained governance, audit trails, and enterprise access control.',
    tag: 'Advanced',
    route: '/deployment/endpoint',
  },
];

const summaryCards = [
  {
    title: 'Assistants',
    icon: ChatBot,
    route: '/deployment/assistant',
    description: 'Deploy and manage AI voice assistants',
  },
  {
    title: 'Endpoints',
    icon: Connect,
    route: '/deployment/endpoint',
    description: 'Manage API endpoints and governance',
  },
  {
    title: 'Integrations',
    icon: Plug,
    route: '/integration/models',
    description: 'Connect AI providers and credentials',
  },
  {
    title: 'Observability',
    icon: Activity,
    route: '/logs',
    description: 'Monitor logs, traces, and conversations',
  },
];

export const HomePage = () => {
  const { user } = useCurrentCredential();
  const navigation = useGlobalNavigation();

  return (
    <div className="flex-1 overflow-auto flex flex-col min-h-0">
      {/* ── Page header — IBM Dashboard style ── */}
      <div className="px-6 pt-6 pb-6 flex items-start justify-between">
        <h1 className="text-2xl font-light tracking-tight">Dashboard</h1>
        <PrimaryButton
          size="md"
          onClick={() => navigation.goToCreateAssistant()}
        >
          Create assistant
        </PrimaryButton>
      </div>

      {/* ── Quick start cards — horizontal scroll ── */}
      <section className="px-6 pb-6">
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-5 gap-4">
          {quickStart.map((item, idx) => (
            <ClickableTile
              key={idx}
              href={item.route}
              className={cn(
                '!rounded-none !h-[320px] !flex !flex-col !p-5 !border-0 !bg-white dark:!bg-gray-950',
                item.featured &&
                  '!bg-gradient-to-b !from-blue-600 !to-indigo-700 dark:!from-blue-700 dark:!to-indigo-800',
              )}
            >
              <h3 className={cn(
                'text-lg font-semibold mb-3',
                item.featured && '!text-2xl !font-light !text-white',
              )}>
                {item.title}
              </h3>
              <p className={cn(
                'text-sm leading-relaxed flex-1',
                item.featured
                  ? 'text-white/90'
                  : 'text-gray-500 dark:text-gray-400',
              )}>
                {item.description}
              </p>
              <div className="mt-auto pt-4 flex items-center justify-between">
                <Tag size="md" type={item.featured ? 'blue' : 'cool-gray'}>
                  {item.tag}
                </Tag>
                <Launch
                  size={16}
                  className={
                    item.featured
                      ? 'text-white/70'
                      : 'text-gray-400 dark:text-gray-500'
                  }
                />
              </div>
            </ClickableTile>
          ))}
        </div>
      </section>


      {/* ── Footer ── */}
      <footer className="mt-auto shrink-0 border-t border-gray-200 dark:border-gray-800 px-6 py-4 flex flex-col sm:flex-row justify-between items-start sm:items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
        <p>
          Need help?{' '}
          <Link href="mailto:contact@rapida.ai" inline>
            contact@rapida.ai
          </Link>
        </p>
        <div className="flex items-center gap-4">
          <span>© 2025 Rapida.ai</span>
          <Link href="/static/privacy-policy" inline>Privacy Policy</Link>
          <Link href="/static/terms" inline>Terms</Link>
        </div>
      </footer>
    </div>
  );
};
