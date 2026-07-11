package processor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"floating-translator/internal/config"
	"floating-translator/internal/filter"
	"floating-translator/internal/logger"
	"floating-translator/internal/translator"
)

type translatorFunc func(context.Context, string) (translator.Result, error)

func (f translatorFunc) Translate(ctx context.Context, text string) (translator.Result, error) {
	return f(ctx, text)
}

func TestLatestTextCancelsPreviousTranslation(t *testing.T) {
	firstStarted := make(chan struct{})
	firstCancelled := make(chan struct{})
	events := make(chan Event, 2)
	translation := translatorFunc(func(ctx context.Context, text string) (translator.Result, error) {
		if text == "First English sentence." {
			close(firstStarted)
			<-ctx.Done()
			close(firstCancelled)
			return translator.Result{}, ctx.Err()
		}
		return translator.Result{Text: "第二句", Model: "fake", DurationMS: 1}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.Handle("First English sentence.")
	waitSignal(t, firstStarted, "第一个请求未启动")
	processor.Handle("Second English sentence.")
	waitSignal(t, firstCancelled, "第一个请求未取消")

	event := waitEvent(t, events)
	if event.Text != "第二句" {
		t.Fatalf("event.Text = %q, want 第二句", event.Text)
	}
	select {
	case extra := <-events:
		t.Fatalf("收到迟到事件: %+v", extra)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestDuplicateTextOnlyTranslatesOnce(t *testing.T) {
	var calls atomic.Int32
	events := make(chan Event, 2)
	translation := translatorFunc(func(_ context.Context, _ string) (translator.Result, error) {
		calls.Add(1)
		return translator.Result{Text: "译文", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.Handle("Repeated English sentence.")
	waitEvent(t, events)
	processor.Handle("Repeated English sentence.")
	time.Sleep(30 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("Translate() calls = %d, want 1", got)
	}
}

func TestFailedTranslationCanRetrySameText(t *testing.T) {
	var calls atomic.Int32
	firstFailed := make(chan struct{})
	events := make(chan Event, 1)
	translation := translatorFunc(func(_ context.Context, _ string) (translator.Result, error) {
		if calls.Add(1) == 1 {
			close(firstFailed)
			return translator.Result{}, errors.New("temporary failure")
		}
		return translator.Result{Text: "重试成功", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.Handle("Retry this English sentence.")
	waitSignal(t, firstFailed, "第一次请求未失败")
	time.Sleep(10 * time.Millisecond)
	processor.Handle("Retry this English sentence.")
	event := waitEvent(t, events)

	if event.Text != "重试成功" || calls.Load() != 2 {
		t.Fatalf("event = %+v, calls = %d", event, calls.Load())
	}
}

func TestDisabledProcessorDoesNotTranslate(t *testing.T) {
	var calls atomic.Int32
	translation := translatorFunc(func(_ context.Context, _ string) (translator.Result, error) {
		calls.Add(1)
		return translator.Result{Text: "不应出现"}, nil
	})
	processor := New(context.Background(), logger.NewNop(), nil)
	processor.Configure(filter.New(config.Default().Clipboard), translation, time.Second, false, false)

	processor.Handle("An English sentence.")
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("Translate() calls = %d, want 0", calls.Load())
	}
}

func TestClipboardTranslationDoesNotEmitUnchangedText(t *testing.T) {
	translated := make(chan struct{})
	events := make(chan Event, 1)
	translation := translatorFunc(func(_ context.Context, text string) (translator.Result, error) {
		close(translated)
		return translator.Result{Text: "  " + text + "\n", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.Handle("Unchanged English sentence.")
	waitSignal(t, translated, "翻译器未被调用")
	select {
	case event := <-events:
		t.Fatalf("原文与译文相同时不应弹出字幕: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSelectionTranslationAlwaysEmitsUnchangedText(t *testing.T) {
	events := make(chan Event, 1)
	translation := translatorFunc(func(_ context.Context, text string) (translator.Result, error) {
		return translator.Result{Text: text, Model: "fake"}, nil
	})
	processor := New(context.Background(), logger.NewNop(), func(event Event) {
		events <- event
	})
	processor.Configure(filter.New(config.Default().Clipboard), translation, time.Second, false, false)
	t.Cleanup(processor.Stop)

	processor.HandleSelection("相同文本")
	event := waitEvent(t, events)
	if event.Text != "相同文本" || event.Source != "selection" {
		t.Fatalf("event = %+v", event)
	}
}

func TestClipboardTextDuringSelectionIsProcessedAfterSelection(t *testing.T) {
	selectionStarted := make(chan struct{})
	releaseSelection := make(chan struct{})
	events := make(chan Event, 3)
	translation := translatorFunc(func(_ context.Context, text string) (translator.Result, error) {
		switch text {
		case "selected text":
			close(selectionStarted)
			<-releaseSelection
			return translator.Result{Text: "选区译文", Model: "fake"}, nil
		case "Second English sentence.":
			return translator.Result{Text: "剪贴板译文", Model: "fake"}, nil
		default:
			return translator.Result{Text: "unexpected", Model: "fake"}, nil
		}
	})
	processor := configuredProcessor(t, translation, events)

	processor.HandleSelection("selected text")
	waitSignal(t, selectionStarted, "选区翻译未开始")
	processor.Handle("First English sentence.")
	processor.Handle("Second English sentence.")
	close(releaseSelection)

	selectionEvent := waitEvent(t, events)
	clipboardEvent := waitEvent(t, events)
	if selectionEvent.Source != "selection" || selectionEvent.Text != "选区译文" {
		t.Fatalf("selection event = %+v", selectionEvent)
	}
	if clipboardEvent.Source != "clipboard" || clipboardEvent.Text != "剪贴板译文" {
		t.Fatalf("clipboard event = %+v", clipboardEvent)
	}
}

func TestClipboardTextDuringSelectionReadIsNotDropped(t *testing.T) {
	events := make(chan Event, 1)
	translation := translatorFunc(func(_ context.Context, text string) (translator.Result, error) {
		return translator.Result{Text: "复制内容译文", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.BeginSelection()
	processor.Handle("Copy this English text.")
	processor.EndSelection()

	event := waitEvent(t, events)
	if event.Source != "clipboard" || event.Text != "复制内容译文" {
		t.Fatalf("event = %+v", event)
	}
}

func TestConfigureDuringSelectionReadPreservesPendingClipboard(t *testing.T) {
	events := make(chan Event, 1)
	translation := translatorFunc(func(_ context.Context, text string) (translator.Result, error) {
		return translator.Result{Text: "配置更新后的译文: " + text, Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.BeginSelection()
	processor.Handle("Clipboard text received during selection.")
	processor.Configure(filter.New(config.Default().Clipboard), translation, time.Second, false, true)
	processor.EndSelection()

	event := waitEvent(t, events)
	if event.Source != "clipboard" || event.Text != "配置更新后的译文: Clipboard text received during selection." {
		t.Fatalf("event = %+v", event)
	}
}

func TestConfigureCancelsSelectionAndFlushesPendingClipboard(t *testing.T) {
	selectionStarted := make(chan struct{})
	events := make(chan Event, 1)
	translation := translatorFunc(func(ctx context.Context, text string) (translator.Result, error) {
		if text == "selected text" {
			close(selectionStarted)
			<-ctx.Done()
			return translator.Result{}, ctx.Err()
		}
		return translator.Result{Text: "新配置译文", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.HandleSelection("selected text")
	waitSignal(t, selectionStarted, "选区翻译未开始")
	processor.Handle("Clipboard text received during selection.")
	processor.Configure(filter.New(config.Default().Clipboard), translation, time.Second, false, true)

	event := waitEvent(t, events)
	if event.Source != "clipboard" || event.Text != "新配置译文" {
		t.Fatalf("event = %+v", event)
	}
}

func TestSelectionTranslationFailureEmitsStatus(t *testing.T) {
	events := make(chan Event, 1)
	translation := translatorFunc(func(context.Context, string) (translator.Result, error) {
		return translator.Result{}, errors.New("模拟模型错误")
	})
	processor := configuredProcessor(t, translation, events)

	processor.HandleSelection("Translate selected text.")
	event := waitEvent(t, events)
	if event.Source != "selection" || event.Text == "" {
		t.Fatalf("event = %+v", event)
	}
}

func TestSelectionEmptyTranslationEmitsStatus(t *testing.T) {
	events := make(chan Event, 1)
	translation := translatorFunc(func(context.Context, string) (translator.Result, error) {
		return translator.Result{Text: "  \n", Model: "fake"}, nil
	})
	processor := configuredProcessor(t, translation, events)

	processor.HandleSelection("Translate selected text.")
	event := waitEvent(t, events)
	if event.Source != "selection" || event.Text == "" {
		t.Fatalf("event = %+v", event)
	}
}

func configuredProcessor(t *testing.T, translation translator.Translator, events chan Event) *Processor {
	t.Helper()
	processor := New(context.Background(), logger.NewNop(), func(event Event) {
		events <- event
	})
	processor.Configure(filter.New(config.Default().Clipboard), translation, time.Second, false, true)
	t.Cleanup(processor.Stop)
	return processor
}

func waitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func waitEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("等待翻译事件超时")
		return Event{}
	}
}
