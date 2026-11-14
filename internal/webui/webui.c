#include "webui.h"

#include <stdlib.h>
#include <string.h>
#include <jsc/jsc.h>

struct FpcView {
	GtkWidget *window;
	WebKitWebView *view;
	gboolean loaded;
};

static gboolean on_delete(GtkWidget *widget, GdkEvent *event, gpointer data)
{
	(void)widget;
	(void)event;
	(void)data;
	/* Go decides: quit (no tray on Linux) or hide. Always stop GTK destroying
	   the window before Go has closed the engine. */
	goWebuiCloseRequested();
	return TRUE;
}

static void on_load_changed(WebKitWebView *view, WebKitLoadEvent event, gpointer data)
{
	(void)view;
	FpcView *v = data;
	if (event == WEBKIT_LOAD_FINISHED) {
		v->loaded = TRUE;
		goWebuiLoadFinished();
	}
}

static void on_message(WebKitUserContentManager *manager,
                       WebKitJavascriptResult *result,
                       gpointer data)
{
	(void)manager;
	(void)data;
	JSCValue *value = webkit_javascript_result_get_js_value(result);
	if (value == NULL)
		return;
	char *message = jsc_value_to_string(value);
	if (message == NULL)
		return;
	goWebuiMessage(message);
	g_free(message);
}

static gboolean dispatch_cb(gpointer data)
{
	guint64 id = *(guint64 *)data;
	g_free(data);
	goWebuiDispatch(id);
	return G_SOURCE_REMOVE;
}

FpcView *fpc_view_new(const char *title, int width, int height, const char *html, int dark)
{
	int argc = 1;
	char *argv0 = (char *)"forever-pulse-companion";
	char **argv = &argv0;
	if (!gtk_init_check(&argc, &argv))
		return NULL;

	GtkSettings *settings = gtk_settings_get_default();
	if (settings != NULL)
		g_object_set(settings, "gtk-application-prefer-dark-theme", dark ? TRUE : FALSE, NULL);

	FpcView *v = g_new0(FpcView, 1);
	v->window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(v->window), title);
	gtk_window_set_default_size(GTK_WINDOW(v->window), width, height);
	gtk_window_set_resizable(GTK_WINDOW(v->window), FALSE);
	gtk_window_set_position(GTK_WINDOW(v->window), GTK_WIN_POS_CENTER);

	WebKitUserContentManager *manager = webkit_user_content_manager_new();
	webkit_user_content_manager_register_script_message_handler(manager, "fpc");
	g_signal_connect(manager, "script-message-received::fpc", G_CALLBACK(on_message), v);

	v->view = WEBKIT_WEB_VIEW(webkit_web_view_new_with_user_content_manager(manager));
	g_object_unref(manager);
	gtk_container_add(GTK_CONTAINER(v->window), GTK_WIDGET(v->view));

	g_signal_connect(v->window, "delete-event", G_CALLBACK(on_delete), v);
	g_signal_connect(v->view, "load-changed", G_CALLBACK(on_load_changed), v);

	webkit_web_view_load_html(v->view, html, NULL);
	/* Réalisé sans être montré : la page et le pont JS existent déjà quand
	   --tray démarre caché, sans faire clignoter de fenêtre. */
	gtk_widget_realize(v->window);
	return v;
}

void fpc_view_free(FpcView *v)
{
	if (v == NULL)
		return;
	if (v->window != NULL)
		gtk_widget_destroy(v->window);
	g_free(v);
}

void fpc_show(FpcView *v)
{
	if (v != NULL)
		gtk_widget_show_all(v->window);
}

void fpc_hide(FpcView *v)
{
	if (v != NULL)
		gtk_widget_hide(v->window);
}

void fpc_present(FpcView *v)
{
	if (v == NULL)
		return;
	gtk_widget_show_all(v->window);
	gtk_window_present(GTK_WINDOW(v->window));
}

void fpc_iconify(FpcView *v)
{
	if (v != NULL)
		gtk_window_iconify(GTK_WINDOW(v->window));
}

void fpc_resize(FpcView *v, int width, int height)
{
	if (v != NULL)
		gtk_window_resize(GTK_WINDOW(v->window), width, height);
}

void fpc_run(FpcView *v)
{
	(void)v;
	gtk_main();
}

void fpc_quit(FpcView *v)
{
	(void)v;
	gtk_main_quit();
}

void fpc_eval(FpcView *v, const char *js)
{
	if (v == NULL || js == NULL || !v->loaded)
		return;
	webkit_web_view_evaluate_javascript(v->view, js, -1, NULL, NULL, NULL, NULL, NULL);
}

void fpc_dispatch(FpcView *v, guint64 id)
{
	(void)v;
	guint64 *data = g_new(guint64, 1);
	*data = id;
	g_idle_add_full(G_PRIORITY_DEFAULT_IDLE, dispatch_cb, data, NULL);
}

static void add_button(GtkDialog *dialog, const char *label, int response)
{
	gtk_dialog_add_button(dialog, label, response);
}

char *fpc_choose_folder(FpcView *v, const char *title, const char *initial)
{
	if (v == NULL)
		return NULL;
	GtkWidget *dialog = gtk_file_chooser_dialog_new(
		title, GTK_WINDOW(v->window), GTK_FILE_CHOOSER_ACTION_SELECT_FOLDER,
		"Annuler", GTK_RESPONSE_CANCEL,
		"Choisir", GTK_RESPONSE_ACCEPT,
		NULL);
	if (initial != NULL && initial[0] != '\0')
		gtk_file_chooser_set_current_folder(GTK_FILE_CHOOSER(dialog), initial);

	char *result = NULL;
	if (gtk_dialog_run(GTK_DIALOG(dialog)) == GTK_RESPONSE_ACCEPT) {
		char *chosen = gtk_file_chooser_get_filename(GTK_FILE_CHOOSER(dialog));
		if (chosen != NULL)
			result = g_strdup(chosen);
		g_free(chosen);
	}
	gtk_widget_destroy(dialog);
	return result;
}

int fpc_confirm(FpcView *v, const char *text, const char *action)
{
	if (v == NULL)
		return 0;
	GtkWidget *dialog = gtk_message_dialog_new(
		GTK_WINDOW(v->window), GTK_DIALOG_MODAL, GTK_MESSAGE_QUESTION,
		GTK_BUTTONS_NONE, "%s", text);
	gtk_window_set_title(GTK_WINDOW(dialog), "Forever Pulse Companion");
	add_button(GTK_DIALOG(dialog), "Annuler", GTK_RESPONSE_CANCEL);
	add_button(GTK_DIALOG(dialog), action, GTK_RESPONSE_ACCEPT);
	gtk_dialog_set_default_response(GTK_DIALOG(dialog), GTK_RESPONSE_CANCEL);
	int response = gtk_dialog_run(GTK_DIALOG(dialog));
	gtk_widget_destroy(dialog);
	return response == GTK_RESPONSE_ACCEPT ? 1 : 0;
}

void fpc_info(FpcView *v, const char *text)
{
	if (v == NULL)
		return;
	GtkWidget *dialog = gtk_message_dialog_new(
		GTK_WINDOW(v->window), GTK_DIALOG_MODAL, GTK_MESSAGE_INFO,
		GTK_BUTTONS_OK, "%s", text);
	gtk_window_set_title(GTK_WINDOW(dialog), "Forever Pulse Companion");
	gtk_dialog_run(GTK_DIALOG(dialog));
	gtk_widget_destroy(dialog);
}

void fpc_set_icon(FpcView *v, const unsigned char *data, unsigned long len)
{
	if (v == NULL || data == NULL || len == 0)
		return;
	GdkPixbufLoader *loader = gdk_pixbuf_loader_new();
	if (gdk_pixbuf_loader_write(loader, data, len, NULL) &&
	    gdk_pixbuf_loader_close(loader, NULL)) {
		GdkPixbuf *pixbuf = gdk_pixbuf_loader_get_pixbuf(loader);
		if (pixbuf != NULL)
			gtk_window_set_icon(GTK_WINDOW(v->window), pixbuf);
	}
	g_object_unref(loader);
}

void fpc_free_string(char *s)
{
	g_free(s);
}
