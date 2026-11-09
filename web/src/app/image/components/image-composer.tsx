"use client";
import {
  ArrowUp,
  Bot,
  Check,
  ChevronDown,
  Image as ImageIcon,
  ImagePlus,
  MessageCircle,
  Minus,
  Plus,
  RectangleHorizontal,
  SlidersHorizontal,
  Store,
  X,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from "react";

import { ImageLightbox } from "@/components/image-lightbox";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import {
  CUSTOM_IMAGE_ASPECT_RATIO,
  IMAGE_ASPECT_RATIO_OPTIONS,
  IMAGE_RESOLUTION_OPTIONS,
  IMAGE_SIZE_MODE_OPTIONS,
  buildImageSize,
  formatImageSizeDisplay,
  getActiveImageAspectRatio,
  isHighResolutionImageSize,
  parseImageRatio,
  type ImageAspectRatio,
  type ImageResolution,
  type ImageSizeMode,
} from "@/app/image/image-options";
import {
  CHAT_MODEL_OPTIONS,
  IMAGE_CREATION_MODEL_OPTIONS,
  IMAGE_MODEL_ROUTE_DETAILS,
  IMAGE_OUTPUT_FORMAT_OPTIONS,
  supportsImageOutputControls,
  supportsImageOutputCompression,
  supportsStructuredImageParameters,
  usesCodexImageRoute,
  usesOfficialImageRoute,
  type ImageModel,
  type ImageOutputFormat,
} from "@/lib/api";
import { cn } from "@/lib/utils";

type ImageComposerProps = {
  composerMode: "chat" | "image";
  prompt: string;
  imageCount: string;
  imageModel: string;
  imageSizeMode: ImageSizeMode;
  imageAspectRatio: ImageAspectRatio;
  imageResolution: ImageResolution;
  imageCustomRatio: string;
  imageCustomWidth: string;
  imageCustomHeight: string;
  imageOutputFormat: ImageOutputFormat;
  imageOutputCompression: string;
  highResolutionHint?: ReactNode;
  billingSummary: string;
  estimatedBillingUnits: number;
  billingBlocked: boolean;
  referenceImages: Array<{ name: string; dataUrl: string }>;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  fileInputRef: RefObject<HTMLInputElement | null>;
  onComposerModeChange: (mode: "chat" | "image") => void;
  onPromptChange: (value: string) => void;
  onImageCountChange: (value: string) => void;
  onImageModelChange: (value: string) => void;
  onImageSizeModeChange: (value: ImageSizeMode) => void;
  onImageAspectRatioChange: (value: ImageAspectRatio) => void;
  onImageResolutionChange: (value: ImageResolution) => void;
  onImageCustomRatioChange: (value: string) => void;
  onImageCustomWidthChange: (value: string) => void;
  onImageCustomHeightChange: (value: string) => void;
  onImageOutputFormatChange: (value: ImageOutputFormat) => void;
  onImageOutputCompressionChange: (value: string) => void;
  onSubmit: () => void | Promise<void>;
  onOpenPromptMarket: () => void;
  onReferenceImageChange: (files: File[]) => void | Promise<void>;
  onRemoveReferenceImage: (index: number) => void;
  /** "inline" = empty-state hero under the page header (menus open downward); "docked" = pinned to the bottom. */
  placement?: "inline" | "docked";
};

const PROMPT_AREA_MIN_HEIGHT = 74;
const PROMPT_AREA_DEFAULT_HEIGHT = 104;
const PROMPT_AREA_MAX_HEIGHT = 320;
const PROMPT_AREA_KEYBOARD_STEP = 16;
const IMAGE_FILE_EXTENSION_PATTERN = /\.(avif|bmp|gif|heic|heif|jpeg|jpg|png|svg|webp)$/i;

function getPromptAreaMaxHeight() {
  if (typeof window === "undefined") {
    return PROMPT_AREA_MAX_HEIGHT;
  }
  return Math.max(PROMPT_AREA_MIN_HEIGHT, Math.min(PROMPT_AREA_MAX_HEIGHT, Math.floor(window.innerHeight * 0.42)));
}

function clampPromptAreaHeight(height: number) {
  return Math.min(Math.max(height, PROMPT_AREA_MIN_HEIGHT), getPromptAreaMaxHeight());
}

function isImageFile(file: File) {
  return file.type.startsWith("image/") || IMAGE_FILE_EXTENSION_PATTERN.test(file.name);
}

function getImageFiles(files: FileList | File[]) {
  return Array.from(files).filter(isImageFile);
}

function hasDraggedFiles(dataTransfer: DataTransfer) {
  return Array.from(dataTransfer.types).includes("Files");
}

function hasDraggedImage(dataTransfer: DataTransfer) {
  if (!hasDraggedFiles(dataTransfer)) {
    return false;
  }

  const items = Array.from(dataTransfer.items);
  if (items.length === 0) {
    return true;
  }

  return items.some((item) => item.kind === "file" && (item.type === "" || item.type.startsWith("image/")));
}

function ImageComposerDock({ children }: { children: ReactNode }) {
  return (
    <div className="w-full">{children}</div>
  );
}

const imageSettingsRowClass = "flex min-h-8 min-w-0 items-center justify-between gap-3";
const imageSettingsLabelClass = "shrink-0 text-[13px] text-muted-foreground";

// Toolbar 比例 menu: "" = Auto (no fixed ratio written / sent).
const TOOLBAR_RATIO_OPTIONS: ReadonlyArray<{ value: ImageAspectRatio; label: string }> = [
  { value: "", label: "Auto" },
  ...IMAGE_ASPECT_RATIO_OPTIONS.filter((option) => option.value !== "" && option.value !== CUSTOM_IMAGE_ASPECT_RATIO).map((option) => ({
    value: option.value,
    label: option.value,
  })),
];

type ImageSettingsMenuOption<Value extends string> = {
  value: Value;
  label: string;
  description?: string;
};

function ImageSettingsPopoverMenu<Value extends string>({
  label,
  value,
  valueLabel,
  options,
  open,
  onOpenChange,
  onValueChange,
  align = "end",
  contentClassName,
  triggerTitle,
}: {
  label: string;
  value: Value;
  valueLabel: string;
  options: ReadonlyArray<ImageSettingsMenuOption<Value>>;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onValueChange: (value: Value) => void;
  align?: "start" | "center" | "end";
  contentClassName?: string;
  triggerTitle?: string;
}) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex h-7 min-w-0 flex-1 items-center justify-end gap-1 bg-transparent text-right text-xs font-semibold text-[#18181b] dark:text-foreground"
          aria-label={`选择${label}，当前 ${valueLabel}`}
          aria-expanded={open}
          title={triggerTitle}
        >
          <span className="truncate">{valueLabel}</span>
          <ChevronDown className={cn("size-4 shrink-0 opacity-60 transition", open && "rotate-180")} />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align={align}
        side="top"
        sideOffset={8}
        collisionPadding={12}
        className={cn(
          "z-[120] max-h-[min(var(--radix-popover-content-available-height),14rem)] w-[min(24rem,calc(100vw-2rem))] overflow-x-hidden overflow-y-auto overscroll-contain rounded-lg border-input bg-card p-1.5 shadow-[0_18px_46px_-26px_rgba(15,23,42,0.35)] dark:border-border dark:bg-card dark:shadow-[0_18px_46px_-24px_rgba(0,0,0,0.72)]",
          contentClassName,
        )}
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <div className="grid gap-1" role="listbox" aria-label={label}>
          {options.map((option) => {
            const active = option.value === value;
            return (
              <button
                key={`${label}-${option.value || option.label}`}
                type="button"
                role="option"
                aria-selected={active}
                className={cn(
                  "flex w-full max-w-full items-start justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm text-[#45515e] transition hover:bg-black/[0.05] dark:text-muted-foreground dark:hover:bg-accent/60",
                  active && "bg-black/[0.05] font-medium text-[#18181b] dark:bg-accent dark:text-foreground",
                )}
                title={option.description}
                onClick={() => {
                  onValueChange(option.value);
                  onOpenChange(false);
                }}
              >
                <span className="min-w-0 max-w-full">
                  <span className="block whitespace-normal break-words">{option.label}</span>
                  {option.description ? (
                    <span className="block whitespace-normal break-words text-[11px] font-normal text-[#8e8e93] dark:text-muted-foreground">
                      {option.description}
                    </span>
                  ) : null}
                </span>
                {active ? <Check className="mt-0.5 size-4 shrink-0" /> : null}
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function ImageComposer({
  composerMode,
  prompt,
  imageCount,
  imageModel,
  imageSizeMode,
  imageAspectRatio,
  imageResolution,
  imageCustomRatio,
  imageCustomWidth,
  imageCustomHeight,
  imageOutputFormat,
  imageOutputCompression,
  highResolutionHint,
  billingSummary,
  estimatedBillingUnits,
  billingBlocked,
  referenceImages,
  textareaRef,
  fileInputRef,
  onComposerModeChange,
  onPromptChange,
  onImageCountChange,
  onImageModelChange,
  onImageSizeModeChange,
  onImageAspectRatioChange,
  onImageResolutionChange,
  onImageCustomRatioChange,
  onImageCustomWidthChange,
  onImageCustomHeightChange,
  onImageOutputFormatChange,
  onImageOutputCompressionChange,
  onSubmit,
  onOpenPromptMarket,
  onReferenceImageChange,
  onRemoveReferenceImage,
  placement = "docked",
}: ImageComposerProps) {
  const inline = placement === "inline";
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [lightboxIndex, setLightboxIndex] = useState(0);
  const [isModelMenuOpen, setIsModelMenuOpen] = useState(false);
  const [isAspectRatioMenuOpen, setIsAspectRatioMenuOpen] = useState(false);
  const [isResolutionMenuOpen, setIsResolutionMenuOpen] = useState(false);
  const [isImageSettingsOpen, setIsImageSettingsOpen] = useState(false);
  const [promptAreaHeight, setPromptAreaHeight] = useState(() => (placement === "inline" ? 128 : PROMPT_AREA_DEFAULT_HEIGHT));
  const [isPromptAreaResizing, setIsPromptAreaResizing] = useState(false);
  const [isReferenceImageDragActive, setIsReferenceImageDragActive] = useState(false);
  const composerPanelRef = useRef<HTMLDivElement>(null);
  const composerToolbarRef = useRef<HTMLDivElement>(null);
  const modelMenuRef = useRef<HTMLDivElement>(null);
  const promptAreaResizeRef = useRef<{ pointerOffsetY: number } | null>(null);
  const referenceImageDragDepthRef = useRef(0);
  const lightboxImages = useMemo(
    () => referenceImages.map((image, index) => ({ id: `${image.name}-${index}`, src: image.dataUrl })),
    [referenceImages],
  );
  const modeModelOptions = composerMode === "chat" ? CHAT_MODEL_OPTIONS : IMAGE_CREATION_MODEL_OPTIONS;
  const imageModelLabel = modeModelOptions.find((option) => option.value === imageModel)?.label || imageModel;
  const imageAspectRatioLabel =
    imageAspectRatio === CUSTOM_IMAGE_ASPECT_RATIO
      ? imageCustomRatio.trim() || "自定义比例"
      : IMAGE_ASPECT_RATIO_OPTIONS.find((option) => option.value === imageAspectRatio)?.label || "Auto";
  const imageResolutionLabel =
    IMAGE_RESOLUTION_OPTIONS.find((option) => option.value === imageResolution)?.label || "Auto";
  const compressionSupported = supportsImageOutputCompression(imageOutputFormat);
  const compressionDisabled = !compressionSupported;
  const officialImageRoute = usesOfficialImageRoute(imageModel);
  const structuredImageParameters = supportsStructuredImageParameters(imageModel);
  const outputControlsSupported = supportsImageOutputControls(imageModel);
  const availableImageSizeModeOptions = structuredImageParameters
    ? IMAGE_SIZE_MODE_OPTIONS
    : IMAGE_SIZE_MODE_OPTIONS.filter((option) => option.value !== "custom");
  const effectiveImageSizeMode = structuredImageParameters || imageSizeMode !== "custom" ? imageSizeMode : "auto";
  const effectiveImageResolution = structuredImageParameters ? imageResolution : "auto";
  const imageCountValue = Math.min(10, Math.max(1, Math.trunc(Number(imageCount)) || 1));
  const toolbarRatioValue: ImageAspectRatio = effectiveImageSizeMode === "ratio" ? imageAspectRatio : "";
  const toolbarRatioLabel =
    effectiveImageSizeMode === "custom"
      ? "自定义"
      : toolbarRatioValue === CUSTOM_IMAGE_ASPECT_RATIO
        ? imageCustomRatio.trim() || "自定义"
        : toolbarRatioValue || "Auto";
  const submitLabel = composerMode === "chat" ? "发送对话" : referenceImages.length > 0 ? "编辑图片" : "生成图片";
  const computedImageSize = useMemo(
    () =>
      buildImageSize({
        mode: effectiveImageSizeMode,
        aspectRatio: imageAspectRatio,
        resolution: effectiveImageResolution,
        customRatio: imageCustomRatio,
        customWidth: imageCustomWidth,
        customHeight: imageCustomHeight,
      }),
    [effectiveImageResolution, effectiveImageSizeMode, imageAspectRatio, imageCustomHeight, imageCustomRatio, imageCustomWidth],
  );
  const activeImageAspectRatio = getActiveImageAspectRatio({
    aspectRatio: imageAspectRatio,
    customRatio: imageCustomRatio,
  });
  const isCustomRatioInvalid =
    effectiveImageSizeMode === "ratio" && imageAspectRatio === CUSTOM_IMAGE_ASPECT_RATIO && !parseImageRatio(imageCustomRatio);
  const sizePreviewLabel = computedImageSize
    ? formatImageSizeDisplay(computedImageSize)
    : effectiveImageSizeMode === "auto" || (effectiveImageSizeMode === "ratio" && effectiveImageResolution === "auto" && !isCustomRatioInvalid)
      ? "Auto"
      : "尺寸无效";
  const sizeIsHighResolution = Boolean(computedImageSize && isHighResolutionImageSize(computedImageSize));
  useEffect(() => {
    if (composerMode === "chat") {
      setIsImageSettingsOpen(false);
      setIsAspectRatioMenuOpen(false);
      setIsResolutionMenuOpen(false);
    }
  }, [composerMode]);

  useEffect(() => {
    if (!isModelMenuOpen) {
      return;
    }
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!modelMenuRef.current?.contains(target)) {
        setIsModelMenuOpen(false);
      }
    };
    window.addEventListener("mousedown", handlePointerDown);
    return () => {
      window.removeEventListener("mousedown", handlePointerDown);
    };
  }, [isModelMenuOpen]);

  useEffect(() => {
    const handleResize = () => {
      setPromptAreaHeight((height) => clampPromptAreaHeight(height));
    };

    window.addEventListener("resize", handleResize);
    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, []);

  useEffect(() => {
    if (!isPromptAreaResizing) {
      return;
    }

    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = "ns-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
    };
  }, [isPromptAreaResizing]);

  const handleTextareaPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const imageFiles = getImageFiles(event.clipboardData.files);
    if (imageFiles.length === 0) {
      return;
    }

    event.preventDefault();
    void onReferenceImageChange(imageFiles);
  };

  const addReferenceImages = (files: File[]) => {
    const imageFiles = getImageFiles(files);
    if (imageFiles.length === 0) {
      return;
    }

    void onReferenceImageChange(imageFiles);
  };

  const resetReferenceImageDragState = () => {
    referenceImageDragDepthRef.current = 0;
    setIsReferenceImageDragActive(false);
  };

  const handleReferenceImageDragEnter = (event: DragEvent<HTMLDivElement>) => {
    if (!hasDraggedImage(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    referenceImageDragDepthRef.current += 1;
    setIsReferenceImageDragActive(true);
    event.dataTransfer.dropEffect = "copy";
  };

  const handleReferenceImageDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (!hasDraggedImage(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    setIsReferenceImageDragActive(true);
    event.dataTransfer.dropEffect = "copy";
  };

  const handleReferenceImageDragLeave = (event: DragEvent<HTMLDivElement>) => {
    if (!hasDraggedImage(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    referenceImageDragDepthRef.current = Math.max(0, referenceImageDragDepthRef.current - 1);
    if (referenceImageDragDepthRef.current === 0) {
      setIsReferenceImageDragActive(false);
    }
  };

  const handleReferenceImageDrop = (event: DragEvent<HTMLDivElement>) => {
    if (!hasDraggedFiles(event.dataTransfer)) {
      return;
    }

    event.preventDefault();
    resetReferenceImageDragState();
    addReferenceImages(Array.from(event.dataTransfer.files));
  };

  const handlePromptResizeStart = (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    const handleRect = event.currentTarget.getBoundingClientRect();
    promptAreaResizeRef.current = {
      pointerOffsetY: event.clientY - handleRect.top,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
    setIsPromptAreaResizing(true);
  };

  const handlePromptResizeMove = (event: PointerEvent<HTMLButtonElement>) => {
    const resizeState = promptAreaResizeRef.current;
    if (!resizeState) {
      return;
    }

    event.preventDefault();
    const panelRect = composerPanelRef.current?.getBoundingClientRect();
    const toolbarHeight = composerToolbarRef.current?.getBoundingClientRect().height ?? 0;
    if (!panelRect) {
      return;
    }

    const handleHeight = event.currentTarget.getBoundingClientRect().height;
    const nextHeight = panelRect.bottom - toolbarHeight - handleHeight - event.clientY + resizeState.pointerOffsetY;
    setPromptAreaHeight(clampPromptAreaHeight(nextHeight));
  };

  const handlePromptResizeEnd = (event: PointerEvent<HTMLButtonElement>) => {
    if (!promptAreaResizeRef.current) {
      return;
    }

    promptAreaResizeRef.current = null;
    setIsPromptAreaResizing(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  };

  const handlePromptResizeKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setPromptAreaHeight((height) => clampPromptAreaHeight(height + PROMPT_AREA_KEYBOARD_STEP));
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setPromptAreaHeight((height) => clampPromptAreaHeight(height - PROMPT_AREA_KEYBOARD_STEP));
      return;
    }
    if (event.key === "Home") {
      event.preventDefault();
      setPromptAreaHeight(PROMPT_AREA_MIN_HEIGHT);
      return;
    }
    if (event.key === "End") {
      event.preventDefault();
      setPromptAreaHeight(getPromptAreaMaxHeight());
    }
  };

  const handlePickReferenceImage = () => {
    fileInputRef.current?.click();
  };

  const handleImageSettingsOpenChange = (open: boolean) => {
    setIsImageSettingsOpen(open);
    if (!open) {
      setIsAspectRatioMenuOpen(false);
      setIsResolutionMenuOpen(false);
    }
  };

  return (
    <ImageComposerDock>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(event) => {
          const files = Array.from(event.target.files || []);
          if (files.length === 0) {
            return;
          }
          addReferenceImages(files);
        }}
      />

      {referenceImages.length > 0 ? (
        <div className="hide-scrollbar mb-2 flex max-h-20 gap-2 overflow-x-auto px-1 py-1 sm:mb-3">
          {referenceImages.map((image, index) => (
            <div key={`${image.name}-${index}`} className="relative size-14 shrink-0 sm:size-16">
              <button
                type="button"
                onClick={() => {
                  setLightboxIndex(index);
                  setLightboxOpen(true);
                }}
                className="group size-14 overflow-hidden rounded-sm border border-stone-200 bg-stone-50 transition hover:border-stone-300 sm:size-16"
                aria-label={`预览参考图 ${image.name || index + 1}`}
              >
                <img
                  src={image.dataUrl}
                  alt={image.name || `参考图 ${index + 1}`}
                  className="h-full w-full object-cover"
                />
              </button>
              <button
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  onRemoveReferenceImage(index);
                }}
                className="absolute -right-1 -top-1 z-10 inline-flex size-5 items-center justify-center rounded-sm border border-stone-200 bg-card text-stone-500 shadow-sm transition hover:border-stone-300 hover:text-stone-800"
                aria-label={`移除参考图 ${image.name || index + 1}`}
              >
                <X className="size-3" />
              </button>
            </div>
          ))}
        </div>
      ) : null}

      <div
        ref={composerPanelRef}
        className={cn(
          "relative overflow-visible rounded-lg border border-input bg-card transition-colors focus-within:border-ink-4/70",
          isReferenceImageDragActive &&
            "border-[#1456f0] bg-[#eef4ff]/95 dark:border-sky-500/70 dark:bg-sky-950/45 sm:border-[#1456f0] sm:bg-[#eef4ff]/95 sm:dark:border-sky-500/70 sm:dark:bg-sky-950/45",
        )}
        onDragEnter={handleReferenceImageDragEnter}
        onDragOver={handleReferenceImageDragOver}
        onDragLeave={handleReferenceImageDragLeave}
        onDrop={handleReferenceImageDrop}
      >
        {isReferenceImageDragActive ? (
          <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-sm border-2 border-dashed border-[#1456f0]/70 bg-white/70 text-sm font-medium text-[#1456f0] backdrop-blur-sm dark:border-sky-400/70 dark:bg-background/70 dark:text-sky-300 sm:rounded-sm">
            <span className="inline-flex items-center gap-2 rounded-sm bg-white/90 px-4 py-2 shadow-[0_10px_30px_-18px_rgba(15,23,42,0.5)] dark:bg-card/90">
              <ImagePlus className="size-4" />
              松开上传图片
            </span>
          </div>
        ) : null}
        <button
          type="button"
          className={cn(
            "hidden",
            isPromptAreaResizing && "cursor-row-resize",
          )}
          onPointerDown={handlePromptResizeStart}
          onPointerMove={handlePromptResizeMove}
          onPointerUp={handlePromptResizeEnd}
          onPointerCancel={handlePromptResizeEnd}
          onLostPointerCapture={() => {
            promptAreaResizeRef.current = null;
            setIsPromptAreaResizing(false);
          }}
          onKeyDown={handlePromptResizeKeyDown}
          aria-label="调整提示词输入区域高度"
          title="拖动调整输入区域高度"
        >
          <span className="h-1 w-10 rounded-sm bg-[#8e8e93]/40 dark:bg-muted-foreground/35" />
        </button>
        <div
          className="cursor-text"
          onClick={() => {
            textareaRef.current?.focus();
          }}
        >
          <ImageLightbox
            images={lightboxImages}
            currentIndex={lightboxIndex}
            open={lightboxOpen}
            onOpenChange={setLightboxOpen}
            onIndexChange={setLightboxIndex}
          />
          <Textarea
            ref={textareaRef}
            value={prompt}
            onChange={(event) => onPromptChange(event.target.value)}
            onPaste={handleTextareaPaste}
            placeholder={
              composerMode === "chat"
                ? "输入消息与AI聊天"
                : referenceImages.length > 0
                ? "描述你希望如何修改参考图"
                : "输入你想要生成的画面，也可直接粘贴图片"
            }
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void onSubmit();
              }
            }}
            className="min-h-[96px] resize-none rounded-none border-0 bg-transparent px-4 pt-4 pb-2 text-[17px] leading-7 text-foreground shadow-none placeholder:text-muted-foreground/80 focus-visible:ring-0 sm:min-h-0 sm:px-5 sm:pt-4 sm:pb-2 sm:text-[15px] sm:leading-7"
            style={{ height: promptAreaHeight }}
          />

          <div
            ref={composerToolbarRef}
            className="bg-transparent px-3 pt-1 pb-3 sm:px-3 sm:pt-1 sm:pb-3"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 sm:gap-3">
              <div className="flex min-w-0 flex-nowrap items-center gap-1.5 sm:gap-1">
                <div className="inline-flex h-9 shrink-0 items-center gap-0.5 rounded-sm bg-transparent p-0 text-[13px] font-medium text-muted-foreground sm:mr-2 sm:border sm:border-input sm:p-0.5">
                  {[
                    { value: "chat" as const, label: "对话", icon: MessageCircle },
                    { value: "image" as const, label: "作画", icon: ImageIcon },
                  ].map((option) => {
                    const Icon = option.icon;
                    const active = composerMode === option.value;
                    return (
                      <button
                        key={option.value}
                        type="button"
                        className={cn(
                          "inline-flex size-9 items-center justify-center gap-2 rounded-[6px] transition sm:h-full sm:w-auto sm:px-3",
                          active ? "bg-secondary text-foreground" : "hover:text-foreground",
                        )}
                        onClick={() => onComposerModeChange(option.value)}
                        aria-pressed={active}
                        aria-label={option.label}
                        title={option.label}
                      >
                        <Icon className="size-4" />
                        <span className="hidden sm:inline">{option.label}</span>
                      </button>
                    );
                  })}
                </div>
                <div ref={modelMenuRef} className="relative shrink-0">
                  <button
                    type="button"
                    className={cn(
                      "inline-flex size-9 shrink-0 items-center justify-center gap-2 rounded-sm text-muted-foreground transition hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 sm:h-9 sm:w-auto sm:px-3 sm:text-[13px] sm:font-medium sm:max-w-[220px]",
                      isModelMenuOpen && "bg-accent text-foreground",
                    )}
                    onClick={() => {
                      setIsModelMenuOpen((open) => !open);
                      setIsAspectRatioMenuOpen(false);
                      setIsResolutionMenuOpen(false);
                    }}
                    aria-expanded={isModelMenuOpen}
                    aria-label={`选择模型，当前 ${imageModelLabel}`}
                    title={`模型：${imageModelLabel}`}
                  >
                    <Bot className="size-5 shrink-0 sm:hidden" />
                    <span className="hidden shrink-0 sm:inline">模型</span>
                    <span className="hidden min-w-0 flex-1 truncate text-left text-foreground sm:inline">
                      {imageModelLabel}
                    </span>
                    <ChevronDown className={cn("hidden size-3.5 shrink-0 opacity-60 transition sm:block", isModelMenuOpen && "rotate-180")} />
                  </button>
                  {isModelMenuOpen ? (
                    <div className={cn("absolute left-0 z-[80] max-h-[45dvh] w-[min(14rem,calc(100vw-2rem))] overflow-y-auto rounded-lg border border-input bg-card p-1 sm:w-[218px]", inline ? "top-[calc(100%+8px)]" : "bottom-[calc(100%+8px)]")}>
                      {modeModelOptions.map((option) => {
                        const active = option.value === imageModel;
                        return (
                          <button
                            key={option.value}
                            type="button"
                            className={cn(
                              "flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm text-[#45515e] transition hover:bg-black/[0.05] dark:text-muted-foreground dark:hover:bg-accent/60",
                              active && "bg-black/[0.05] font-medium text-[#18181b] dark:bg-accent dark:text-foreground",
                            )}
                            onClick={() => {
                              onImageModelChange(option.value);
                              setIsModelMenuOpen(false);
                            }}
                          >
                            <span className="min-w-0">
                              <span className="block truncate">{option.label}</span>
                              {composerMode === "image" && IMAGE_MODEL_ROUTE_DETAILS[option.value] ? (
                                <span className="block truncate text-[11px] font-normal text-[#8e8e93] dark:text-muted-foreground">
                                  {IMAGE_MODEL_ROUTE_DETAILS[option.value]?.routeLabel}
                                </span>
                              ) : null}
                            </span>
                            {active ? <Check className="size-4 shrink-0" /> : null}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                </div>
                {composerMode === "image" ? (
                  <Popover
                    open={isAspectRatioMenuOpen}
                    onOpenChange={(open) => {
                      setIsAspectRatioMenuOpen(open);
                      if (open) {
                        setIsModelMenuOpen(false);
                        setIsImageSettingsOpen(false);
                      }
                    }}
                  >
                    <PopoverTrigger asChild>
                      <button
                        type="button"
                        className={cn(
                          "inline-flex size-9 shrink-0 items-center justify-center gap-2 rounded-sm text-muted-foreground transition hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 sm:h-9 sm:w-auto sm:px-3 sm:text-[13px] sm:font-medium",
                          isAspectRatioMenuOpen && "bg-accent text-foreground",
                        )}
                        aria-label={`选择比例，当前 ${toolbarRatioLabel}`}
                        aria-expanded={isAspectRatioMenuOpen}
                        title={`比例：${toolbarRatioLabel}`}
                      >
                        <RectangleHorizontal className="size-4 shrink-0" />
                        <span className="hidden shrink-0 sm:inline">比例</span>
                        <span className="hidden text-foreground tabular-nums sm:inline">{toolbarRatioLabel}</span>
                        <ChevronDown className={cn("hidden size-3.5 shrink-0 opacity-60 transition sm:block", isAspectRatioMenuOpen && "rotate-180")} />
                      </button>
                    </PopoverTrigger>
                    <PopoverContent
                      align="start"
                      side={inline ? "bottom" : "top"}
                      sideOffset={8}
                      collisionPadding={12}
                      className="z-[80] w-[132px] rounded-sm border-input bg-card p-1 shadow-[0_18px_46px_-26px_rgba(15,23,42,0.35)] dark:border-border dark:shadow-[0_18px_46px_-24px_rgba(0,0,0,0.72)]"
                      onOpenAutoFocus={(event) => event.preventDefault()}
                    >
                      <div className="grid gap-0.5" role="listbox" aria-label="比例">
                        {TOOLBAR_RATIO_OPTIONS.map((option) => {
                          const active = option.value === toolbarRatioValue;
                          return (
                            <button
                              key={option.value || "auto"}
                              type="button"
                              role="option"
                              aria-selected={active}
                              className={cn(
                                "flex h-8 w-full items-center justify-between gap-2 rounded-[6px] px-2.5 text-left text-[13px] tabular-nums text-muted-foreground transition hover:bg-accent hover:text-foreground",
                                active && "bg-accent font-medium text-foreground",
                              )}
                              onClick={() => {
                                if (option.value === "") {
                                  onImageSizeModeChange("auto");
                                  onImageAspectRatioChange("");
                                } else {
                                  onImageSizeModeChange("ratio");
                                  onImageAspectRatioChange(option.value);
                                }
                                setIsAspectRatioMenuOpen(false);
                              }}
                            >
                              <span>{option.label}</span>
                              {active ? <Check className="size-3.5 shrink-0" /> : null}
                            </button>
                          );
                        })}
                      </div>
                    </PopoverContent>
                  </Popover>
                ) : null}
                {composerMode === "image" ? (
                  <Popover open={isImageSettingsOpen} onOpenChange={handleImageSettingsOpenChange}>
                    <PopoverTrigger asChild>
                      <button
                        type="button"
                        className={cn(
                          "inline-flex size-9 shrink-0 items-center justify-center gap-2 rounded-sm text-muted-foreground transition hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 sm:h-9 sm:w-auto sm:px-3 sm:text-[13px] sm:font-medium",
                          isImageSettingsOpen && "bg-accent text-foreground",
                        )}
                        aria-label={isImageSettingsOpen ? "收起参数设置" : "显示更多参数设置"}
                        aria-expanded={isImageSettingsOpen}
                        title={isImageSettingsOpen ? "收起参数" : "更多参数"}
                      >
                        <SlidersHorizontal className="size-4" />
                        <span className="hidden sm:inline">参数</span>
                      </button>
                    </PopoverTrigger>
                    <PopoverContent
                      align="start"
                      side={inline ? "bottom" : "top"}
                      sideOffset={8}
                      className="z-[70] w-[min(calc(100vw-1rem),300px)] rounded-sm border-input bg-card p-3 shadow-[0_18px_46px_-26px_rgba(15,23,42,0.35)] dark:border-border dark:bg-card dark:shadow-[0_18px_46px_-24px_rgba(0,0,0,0.72)]"
                      onOpenAutoFocus={(event) => event.preventDefault()}
                    >
                      <div className="flex flex-col gap-2">
                        <div className={imageSettingsRowClass}>
                          <span className={imageSettingsLabelClass}>张数</span>
                          <div className="inline-flex h-8 items-center rounded-sm border border-input">
                            <button
                              type="button"
                              className="inline-flex size-8 items-center justify-center rounded-l-sm text-muted-foreground transition hover:bg-accent hover:text-foreground disabled:opacity-40"
                              aria-label="减少张数"
                              disabled={imageCountValue <= 1}
                              onClick={() => onImageCountChange(String(Math.max(1, imageCountValue - 1)))}
                            >
                              <Minus className="size-3.5" />
                            </button>
                            <Input
                              type="number"
                              inputMode="numeric"
                              min="1"
                              max="10"
                              step="1"
                              value={imageCount}
                              onChange={(event) => onImageCountChange(event.target.value)}
                              aria-label="张数"
                              className="h-8 w-9 rounded-none border-0 border-x border-input bg-transparent px-0 text-center text-[13px] font-medium shadow-none [appearance:textfield] focus-visible:ring-0 [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                            />
                            <button
                              type="button"
                              className="inline-flex size-8 items-center justify-center rounded-r-sm text-muted-foreground transition hover:bg-accent hover:text-foreground disabled:opacity-40"
                              aria-label="增加张数"
                              disabled={imageCountValue >= 10}
                              onClick={() => onImageCountChange(String(Math.min(10, imageCountValue + 1)))}
                            >
                              <Plus className="size-3.5" />
                            </button>
                          </div>
                        </div>
                        {outputControlsSupported ? (
                          <>
                            <div className={imageSettingsRowClass}>
                              <span className={imageSettingsLabelClass}>格式</span>
                              <Select
                                value={imageOutputFormat}
                                onValueChange={(value) => {
                                  const nextFormat = value as ImageOutputFormat;
                                  onImageOutputFormatChange(nextFormat);
                                  if (!supportsImageOutputCompression(nextFormat)) {
                                    onImageOutputCompressionChange("");
                                  }
                                }}
                              >
                                <SelectTrigger className="h-8 w-[118px] text-[13px]" aria-label="图片输出格式">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent align="end" side="top" sideOffset={8} collisionPadding={12} className="z-[120] w-[160px]">
                                  <SelectGroup>
                                    {IMAGE_OUTPUT_FORMAT_OPTIONS.map((option) => (
                                      <SelectItem key={option.value} value={option.value} className="text-[13px]">
                                        {option.label}
                                      </SelectItem>
                                    ))}
                                  </SelectGroup>
                                </SelectContent>
                              </Select>
                            </div>
                            {compressionSupported ? (
                              <label className={imageSettingsRowClass}>
                                <span className={imageSettingsLabelClass}>压缩率</span>
                                <Input
                                  type="number"
                                  inputMode="numeric"
                                  min="0"
                                  max="100"
                                  step="1"
                                  value={imageOutputCompression}
                                  onChange={(event) => onImageOutputCompressionChange(event.target.value)}
                                  placeholder="0-100"
                                  className="h-8 w-[118px] text-right text-[13px]"
                                />
                              </label>
                            ) : null}
                          </>
                        ) : null}
                      </div>
                    </PopoverContent>
                  </Popover>
                  ) : null}
                <button
                  type="button"
                  className="inline-flex size-9 shrink-0 items-center justify-center gap-2 rounded-sm text-muted-foreground transition hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30 sm:h-9 sm:w-auto sm:px-3 sm:text-[13px] sm:font-medium"
                  onClick={onOpenPromptMarket}
                  aria-label="打开提示词市场"
                  title="提示词市场"
                >
                  <Store className="size-4" />
                  <span className="hidden sm:inline">市场</span>
                </button>
              </div>

              <div className="flex shrink-0 items-center gap-2">
                <span
                  className={cn(
                    "num hidden pr-1 text-xs whitespace-nowrap sm:inline",
                    billingBlocked ? "text-rose-600 dark:text-rose-400" : "text-muted-foreground",
                  )}
                  title={billingSummary}
                >
                  {billingBlocked ? billingSummary : composerMode === "chat" ? "Enter 发送 · Shift+Enter 换行" : `预计消耗 ${estimatedBillingUnits} 图片单位`}
                </span>
                <button
                  type="button"
                  onClick={handlePickReferenceImage}
                  className="inline-flex size-9 items-center justify-center rounded-sm text-muted-foreground transition hover:bg-accent hover:text-foreground"
                  aria-label="上传参考图"
                  title="上传参考图"
                >
                  <Plus className="size-4 sm:hidden" />
                  <ImagePlus className="hidden size-4 sm:block" />
                </button>

                <button
                  type="button"
                  onClick={() => void onSubmit()}
                  disabled={!prompt.trim() || billingBlocked}
                  className="inline-flex size-9 shrink-0 items-center justify-center rounded-sm border border-primary bg-primary text-primary-foreground transition hover:bg-primary/88 disabled:cursor-not-allowed disabled:opacity-45"
                  aria-label={submitLabel}
                  title={billingBlocked ? "用户余额或配额不足" : `${submitLabel}，预计消耗 ${estimatedBillingUnits}`}
                >
                  <ArrowUp className="size-4" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </ImageComposerDock>
  );
}
