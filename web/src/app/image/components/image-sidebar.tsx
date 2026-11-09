"use client";

import { LoaderCircle, Plus, Trash2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { getImageConversationStats, type ImageConversation } from "@/store/image-conversations";

type ImageSidebarProps = {
  conversations: ImageConversation[];
  isLoadingHistory: boolean;
  selectedConversationId: string | null;
  onCreateDraft: () => void;
  onClearHistory: () => void | Promise<void>;
  onSelectConversation: (id: string) => void;
  onDeleteConversation: (id: string) => void | Promise<void>;
  formatConversationTime: (value: string) => string;
  hideActionButtons?: boolean;
};

/**
 * History column: a section title on a rule, then flat text rows separated by hairlines.
 * The title row is 36px tall so its rule lines up with the composer / page header rhythm.
 */
export function ImageSidebar({
  conversations,
  isLoadingHistory,
  selectedConversationId,
  onCreateDraft,
  onClearHistory,
  onSelectConversation,
  onDeleteConversation,
  formatConversationTime,
  hideActionButtons = false,
}: ImageSidebarProps) {
  return (
    <aside className="flex h-full min-h-0 flex-col overflow-hidden">
      {!hideActionButtons ? (
        <div className="flex h-9 shrink-0 items-end justify-between gap-2 border-b border-rule-strong pb-2">
          <h2 className="font-serif text-lg leading-none font-medium text-foreground">历史</h2>
          <button
            type="button"
            onClick={onCreateDraft}
            className="-mr-1 inline-flex h-7 items-center gap-1 rounded-sm px-1.5 text-[13px] text-oxblood transition hover:bg-accent"
          >
            <Plus className="size-3.5" />
            新建
          </button>
        </div>
      ) : null}

      <div className="hide-scrollbar min-h-0 flex-1 overflow-y-auto">
        {isLoadingHistory ? (
          <div className="flex items-center gap-2 py-3 text-[13px] text-muted-foreground">
            <LoaderCircle className="size-3.5 animate-spin" />
            正在读取记录
          </div>
        ) : conversations.length === 0 ? (
          <p className="py-3 text-[13px] leading-6 text-muted-foreground">暂无记录</p>
        ) : (
          <ul className="flex flex-col">
            {conversations.map((conversation) => {
              const active = conversation.id === selectedConversationId;
              const stats = getImageConversationStats(conversation);
              const busy = stats.running + stats.queued;
              return (
                <li key={conversation.id} className="group relative border-b border-border">
                  <button
                    type="button"
                    onClick={() => onSelectConversation(conversation.id)}
                    className={cn(
                      "block w-full py-2.5 pr-7 text-left transition",
                      active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <span
                      className={cn(
                        "block truncate text-[13px] leading-5",
                        active && "font-medium text-foreground",
                      )}
                    >
                      {active ? <span className="mr-1.5 inline-block size-1.5 translate-y-[-2px] rounded-full bg-oxblood align-middle" /> : null}
                      {conversation.title}
                    </span>
                    <span className="num mt-0.5 block text-[11px] leading-4 text-muted-foreground">
                      {formatConversationTime(conversation.updatedAt)} · {conversation.turns.length} 轮
                      {busy > 0 ? <span className="text-oxblood"> · 进行中 {busy}</span> : null}
                    </span>
                  </button>
                  {!hideActionButtons ? (
                    <button
                      type="button"
                      onClick={() => void onDeleteConversation(conversation.id)}
                      className="absolute top-2.5 right-0 inline-flex size-6 items-center justify-center rounded-sm text-muted-foreground opacity-0 transition group-hover:opacity-100 hover:text-rose-600"
                      aria-label="删除会话"
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {!hideActionButtons && conversations.length > 0 ? (
        <button
          type="button"
          onClick={() => void onClearHistory()}
          className="mt-3 shrink-0 self-start text-xs text-muted-foreground transition hover:text-rose-600"
        >
          清空记录
        </button>
      ) : null}
    </aside>
  );
}
