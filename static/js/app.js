(function($) {
    $(document).ready( function() {
		$('#default_accounts_id').change(function() {
		    window.location = "accounts?accounts_id=" + $(this).val();
		});

	    setTimeout(function() {
	        $(".alert").alert('close');
	    }, 2000);

    });
})(jQuery);


