import { useRapidaStore } from '@/hooks';
import { useCredential } from '@/hooks/use-credential';
import React, { useCallback, useEffect } from 'react';
import toast from 'react-hot-toast/headless';
import { BluredWrapper } from '@/app/components/wrapper/blured-wrapper';
import { SearchIconInput } from '@/app/components/form/input/IconInput';
import { KnowledgeDocument } from '@rapidaai/react';
import { useKnowledgeDocumentPageStore } from '@/hooks/use-knowledge-document-page-store';
import { TablePagination } from '@/app/components/base/tables/table-pagination';
import { SingleDocument } from '@/app/pages/knowledge-base/view/documents/single-document';
import { Knowledge } from '@rapidaai/react';
import { PageLoading } from '@/app/components/carbon/loading';
import { EmptyState } from '@/app/components/carbon/empty-state';
import { ScrollableResizableTable } from '@/app/components/data-table';

export function Documents(props: {
  currentKnowledge: Knowledge;
  onAddKnowledgeDocument: () => void;
}) {
  const [userId, token, projectId] = useCredential();
  const rapidaContext = useRapidaStore();
  const knowledgeDocumentAction = useKnowledgeDocumentPageStore();

  const getKnowledgeDocument = useCallback(() => {
    knowledgeDocumentAction.getAllKnowledgeDocument(
      props.currentKnowledge.getId(),
      projectId,
      token,
      userId,
      (err: string) => {
        rapidaContext.hideLoader();
        toast.error(err);
      },
      (data: KnowledgeDocument[]) => {
        rapidaContext.hideLoader();
      },
    );
  }, [props.currentKnowledge]);

  useEffect(() => {
    rapidaContext.showLoader();
    getKnowledgeDocument();
  }, [
    props.currentKnowledge,
    projectId,
    knowledgeDocumentAction.page,
    knowledgeDocumentAction.pageSize,
    knowledgeDocumentAction.criteria,
  ]);

  return (
    <>
      {rapidaContext.loading ? (
        <PageLoading className="h-full grow" />
      ) : knowledgeDocumentAction.documents &&
      knowledgeDocumentAction.documents.length > 0 ? (
        <div className="flex flex-col h-full flex-1">
          <BluredWrapper className="p-0">
            <SearchIconInput className="bg-light-background" />
            <TablePagination
              columns={knowledgeDocumentAction.columns}
              currentPage={knowledgeDocumentAction.page}
              onChangeCurrentPage={knowledgeDocumentAction.setPage}
              totalItem={knowledgeDocumentAction.totalCount}
              pageSize={knowledgeDocumentAction.pageSize}
              onChangePageSize={knowledgeDocumentAction.setPageSize}
              onChangeColumns={knowledgeDocumentAction.setColumns}
            />
          </BluredWrapper>

          <ScrollableResizableTable
            isActionable={false}
            isOptionable={true}
            clms={knowledgeDocumentAction.columns.filter(x => {
              return x.visible;
            })}
          >
            {knowledgeDocumentAction.documents.map((kd, idx) => {
              return (
                <SingleDocument
                  key={`document_row_${idx}`}
                  document={kd}
                  onReload={() => {
                    getKnowledgeDocument();
                  }}
                />
              );
            })}
            {/* </TBody> */}
          </ScrollableResizableTable>
        </div>
      ) : (
        <div className="flex flex-col h-full flex-1 items-center justify-center">
          <EmptyState
            title="No Documents"
            subtitle="There are no documents in knowledge to display"
            action="Add New Document"
            onAction={() => props.onAddKnowledgeDocument()}
          />
        </div>
      )}
    </>
  );
}
